package cli

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/charmbracelet/lipgloss"
)

//go:embed templates/*
var templatesFS embed.FS

// TemplateData holds data for template rendering
type TemplateData struct {
	ProjectName string
	ModuleName  string
	GitHubUser  string
	Databases   []string
	IncludeAuth bool
	HasMongoDB  bool
	HasPostgres bool
	CreateEnv   bool
}

// scaffold creates the project structure based on user configuration
func scaffold(config *ProjectConfig) error {
	// Prepare template data
	data := TemplateData{
		ProjectName: config.Name,
		ModuleName:  fmt.Sprintf("github.com/%s/%s", config.GitHubUser, config.Name),
		GitHubUser:  config.GitHubUser,
		Databases:   config.Database,
		IncludeAuth: config.IncludeAuth,
		HasMongoDB:  contains(config.Database, "mongodb"),
		HasPostgres: contains(config.Database, "postgres"),
		CreateEnv:   config.CreateEnv,
	}

	// Create project directory
	projectPath := config.Path
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	// Define files to create
	files := []struct {
		path     string
		template string
		optional bool
	}{
		{"main.go", "main.go.tmpl", false},
		{"go.mod", "go.mod.tmpl", false},
		{".gitignore", "gitignore.tmpl", false},
		{"README.md", "readme.tmpl", false},
		{"config/config.go", "config.go.tmpl", false},
		{"routes/routes.go", "routes.go.tmpl", false},
	}

	// Add database files
	if data.HasMongoDB || data.HasPostgres {
		files = append(files, struct {
			path     string
			template string
			optional bool
		}{"database/database.go", "database.go.tmpl", false})
	}

	// Add auth files
	if data.IncludeAuth {
		files = append(files,
			struct {
				path     string
				template string
				optional bool
			}{"handlers/auth.go", "handlers_auth.go.tmpl", false},
			struct {
				path     string
				template string
				optional bool
			}{"middleware/auth.go", "middleware_auth.go.tmpl", false},
		)
	}

	// Add .env file if requested
	if data.CreateEnv {
		files = append(files,
			struct {
				path     string
				template string
				optional bool
			}{".env", "env.tmpl", false},
			struct {
				path     string
				template string
				optional bool
			}{".env.example", "env.example.tmpl", false},
		)
	}

	// Create files
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)

	for _, file := range files {
		filePath := filepath.Join(projectPath, file.path)

		// Create directory if needed
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Printf("%s Failed to create directory %s: %v\n", errorStyle.Render("Error"), dir, err)
			continue
		}

		// Read template
		templateContent, err := templatesFS.ReadFile("templates/" + file.template)
		if err != nil {
			if file.optional {
				continue
			}
			return fmt.Errorf("failed to read template %s: %w", file.template, err)
		}

		// Parse and execute template
		tmpl, err := template.New(file.template).Parse(string(templateContent))
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", file.template, err)
		}

		// Create file
		f, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", filePath, err)
		}

		// Execute template
		if err := tmpl.Execute(f, data); err != nil {
			f.Close()
			return fmt.Errorf("failed to execute template %s: %w", file.template, err)
		}
		f.Close()

		fmt.Printf("%s Created %s\n", successStyle.Render("✓"), file.path)
	}

	// Run go mod tidy
	fmt.Println("\n📦 Running go mod tidy...")
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = projectPath
	if err := tidyCmd.Run(); err != nil {
		fmt.Printf("⚠️  Warning: go mod tidy failed: %v\n", err)
		fmt.Println("   Run 'go mod tidy' manually in the project directory")
	} else {
		fmt.Println("✓ Dependencies resolved")
	}

	// Print success message
	fmt.Println()
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 2)

	successMsg := fmt.Sprintf(
		"🎉 Project %s created successfully!\n\n"+
			"Next steps:\n"+
			"  cd %s\n"+
			"  go run main.go",
		config.Name, config.Path,
	)

	fmt.Println(boxStyle.Render(successMsg))

	return nil
}

// contains checks if a string slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

