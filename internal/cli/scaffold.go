package cli

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/charmbracelet/lipgloss"
)

//go:embed templates/*
var templatesFS embed.FS

// TemplateData holds data for template rendering
type TemplateData struct {
	ProjectName   string
	ModuleName    string
	Databases     []string
	IncludeAuth   bool
	HasMongoDB    bool
	HasPostgres   bool
	CreateEnv     bool
}

// scaffold creates the project structure based on user configuration
func scaffold(config *ProjectConfig) error {
	// Prepare template data
	data := TemplateData{
		ProjectName: config.Name,
		ModuleName:  extractModuleName(config.Name),
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

	// Print success message
	fmt.Println()
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(1, 2)

	successMsg := fmt.Sprintf(
		"🎉 Project %s created successfully!\n\n"+
			"📁 cd %s\n"+
			"📦 go mod tidy\n"+
			"🚀 go run main.go",
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

// extractModuleName extracts a Go module name from project name
func extractModuleName(projectName string) string {
	// Remove spaces and convert to lowercase
	name := strings.ToLower(strings.ReplaceAll(projectName, " ", "-"))

	// Add common prefix if not already present
	if !strings.Contains(name, "/") {
		// Use a generic username prefix - user should customize this
		name = "github.com/yourusername/" + name
	}

	return name
}
