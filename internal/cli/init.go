package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

type ProjectConfig struct {
	Name       string
	Path       string
	Database   []string
	IncludeAuth bool
	CreateEnv   bool
}

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize a new CoreGo project",
	Args:  cobra.MaximumNArgs(1),
	Run:   runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) {
	config := ProjectConfig{}

	// If project name provided as argument, use it
	if len(args) > 0 {
		config.Name = args[0]
	}

	// Interactive prompts
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Project name").
				Description("What's your project called?").
				Value(&config.Name).
				Placeholder("my-api"),

			huh.NewInput().
				Title("Project path").
				Description("Where should we create it?").
				Value(&config.Path).
				Placeholder("./my-api"),
		),

		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Database").
				Description("Which database(s) do you want?").
				Options(
					huh.NewOption("MongoDB", "mongodb"),
					huh.NewOption("PostgreSQL", "postgres"),
				).
				Value(&config.Database),

			huh.NewConfirm().
				Title("Include authentication?").
				Value(&config.IncludeAuth),

			huh.NewConfirm().
				Title("Create .env file?").
				Value(&config.CreateEnv),
		),
	)

	err := form.Run()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Set default path if empty
	if config.Path == "" {
		config.Path = "./" + config.Name
	}

	// Call scaffold function to create the project
	if err := scaffold(&config); err != nil {
		fmt.Printf("Error creating project: %v\n", err)
		return
	}
}