package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/pathutil"
)

// ConnectionSecretType represents the type of connection secret.
type ConnectionSecretType int

const (
	ConnectionSecretTypeNone ConnectionSecretType = iota
	ConnectionSecretTypePostgres
	ConnectionSecretTypeMySQL
	ConnectionSecretTypeS3
	ConnectionSecretTypeGCS
	ConnectionSecretTypeR2
	ConnectionSecretTypeHTTP
	ConnectionSecretTypeAzure
)

// String returns a human-readable name for the connection secret type.
func (c ConnectionSecretType) String() string {
	switch c {
	case ConnectionSecretTypePostgres:
		return "PostgreSQL"
	case ConnectionSecretTypeMySQL:
		return "MySQL"
	case ConnectionSecretTypeS3:
		return "AWS S3"
	case ConnectionSecretTypeGCS:
		return "Google Cloud Storage"
	case ConnectionSecretTypeR2:
		return "Cloudflare R2"
	case ConnectionSecretTypeHTTP:
		return "HTTP"
	case ConnectionSecretTypeAzure:
		return "Azure Blob Storage"
	default:
		return "None"
	}
}

// TypeString returns the YAML type string for the connection secret.
func (c ConnectionSecretType) TypeString() string {
	switch c {
	case ConnectionSecretTypePostgres:
		return "postgres"
	case ConnectionSecretTypeMySQL:
		return "mysql"
	case ConnectionSecretTypeS3:
		return "s3"
	case ConnectionSecretTypeGCS:
		return "gcs"
	case ConnectionSecretTypeR2:
		return "r2"
	case ConnectionSecretTypeHTTP:
		return "http"
	case ConnectionSecretTypeAzure:
		return "azure"
	default:
		return ""
	}
}

// ConnectionSecretManifestData holds data for rendering a ConnectionSecret manifest.
type ConnectionSecretManifestData struct {
	Name        string
	Description string
	Constraints []string
	Type        ConnectionSecretType

	// For database types (postgres, mysql)
	PasswordFromEnv string

	// For cloud storage (s3, gcs, r2)
	KeyID     string
	SecretEnv string

	// For HTTP
	Username    string
	Password    string
	BearerToken string

	// For Azure
	ConnectionString string
	AccountKey       string
}

func newAddConnectionSecretCommand() *cobra.Command {
	var (
		flagType        string
		flagPasswordEnv string
		flagKeyID       string
		flagSecretEnv   string
		flagConstraint  []string
		flagOutput      string
		flagAppendTo    string
		flagDesc        string
		flagNoPrompt    bool
		flagOpenEditor  bool
	)

	cmd := &cobra.Command{
		Use:     "connectionsecret [name]",
		Aliases: []string{"secret"},
		Short:   "Create a ConnectionSecret manifest",
		Long: strings.TrimSpace(`
Create a new ConnectionSecret manifest for database and storage credentials.

ConnectionSecret securely stores credentials for:
  - Database connections (PostgreSQL, MySQL)
  - Cloud storage (AWS S3, Google Cloud Storage, Cloudflare R2, Azure)
  - HTTP authentication

Credentials are typically stored as environment variable references
to avoid hardcoding sensitive values in manifest files.
`),
		Example: strings.TrimSpace(`
  # Interactive wizard
  bino add connectionsecret

  # PostgreSQL credentials
  bino add connectionsecret pg_credentials \
    --type postgres \
    --password-env DB_PASSWORD \
    --output secrets/postgres.yaml \
    --no-prompt

  # S3 credentials
  bino add connectionsecret s3_credentials \
    --type s3 \
    --key-id AKIAIOSFODNN7EXAMPLE \
    --secret-env AWS_SECRET_KEY \
    --output secrets/s3.yaml \
    --no-prompt
`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			workdir, err := pathutil.ResolveWorkdir(".")
			if err != nil {
				return ConfigError(err)
			}

			nonInteractive := flagNoPrompt || !isInteractive()

			var name string
			if len(args) > 0 {
				name = args[0]
			}

			// Parse type
			var secretType ConnectionSecretType
			switch strings.ToLower(flagType) {
			case "postgres", "postgresql":
				secretType = ConnectionSecretTypePostgres
			case "mysql":
				secretType = ConnectionSecretTypeMySQL
			case "s3":
				secretType = ConnectionSecretTypeS3
			case "gcs":
				secretType = ConnectionSecretTypeGCS
			case "r2":
				secretType = ConnectionSecretTypeR2
			case "http":
				secretType = ConnectionSecretTypeHTTP
			case "azure":
				secretType = ConnectionSecretTypeAzure
			}

			if nonInteractive {
				var missing []string
				if name == "" {
					missing = append(missing, "name (as argument)")
				}
				if secretType == ConnectionSecretTypeNone {
					missing = append(missing, "--type")
				}
				if flagOutput == "" && flagAppendTo == "" {
					missing = append(missing, "--output or --append-to")
				}
				if len(missing) > 0 {
					return ConfigError(fmt.Errorf("missing required values in non-interactive mode:\n  %s", strings.Join(missing, "\n  ")))
				}
			}

			manifests, err := ScanManifests(ctx, workdir)
			if err != nil {
				return RuntimeError(fmt.Errorf("scan manifests: %w", err))
			}

			data := ConnectionSecretManifestData{
				Name:            name,
				Description:     flagDesc,
				Constraints:     flagConstraint,
				Type:            secretType,
				PasswordFromEnv: flagPasswordEnv,
				KeyID:           flagKeyID,
				SecretEnv:       flagSecretEnv,
			}

			var outputPath string
			var appendMode bool
			if flagAppendTo != "" {
				outputPath = flagAppendTo
				appendMode = true
			} else if flagOutput != "" {
				outputPath = flagOutput
			}

			if nonInteractive {
				return writeConnectionSecretManifest(cmd, workdir, data, outputPath, appendMode)
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Create a new ConnectionSecret manifest.")
			fmt.Fprintln(out, "Press Ctrl+C to cancel at any time.")
			fmt.Fprintln(out)

			// Name
			if data.Name == "" {
				data.Name, err = promptGenericName(reader, out, manifests, "ConnectionSecret")
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			} else {
				if err := ValidateName(data.Name); err != nil {
					return ConfigError(err)
				}
			}

			if data.Description == "" {
				data.Description, _ = addPromptString(reader, out, "Description (optional)", "")
			}

			// Type selection
			if data.Type == ConnectionSecretTypeNone {
				options := []SelectOption{
					{Label: "PostgreSQL", Description: "PostgreSQL database credentials"},
					{Label: "MySQL", Description: "MySQL database credentials"},
					{Label: "AWS S3", Description: "AWS S3 storage credentials"},
					{Label: "Google Cloud Storage", Description: "GCS credentials"},
					{Label: "Cloudflare R2", Description: "R2 storage credentials"},
					{Label: "HTTP", Description: "HTTP authentication"},
					{Label: "Azure Blob Storage", Description: "Azure storage credentials"},
				}

				idx, err := addPromptSelect(reader, out, "What type of credentials?", options, 0)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}

				types := []ConnectionSecretType{
					ConnectionSecretTypePostgres,
					ConnectionSecretTypeMySQL,
					ConnectionSecretTypeS3,
					ConnectionSecretTypeGCS,
					ConnectionSecretTypeR2,
					ConnectionSecretTypeHTTP,
					ConnectionSecretTypeAzure,
				}
				data.Type = types[idx]
			}

			// Type-specific prompts
			if err := promptConnectionSecretDetails(reader, out, &data); err != nil {
				if errors.Is(err, errAddCanceled) {
					fmt.Fprintln(out, "\nCancelled.")
					return nil
				}
				return RuntimeError(err)
			}

			// Constraints
			if len(data.Constraints) == 0 {
				addConstraints, err := addPromptConfirm(reader, out, "Add constraints?", false)
				if err != nil {
					return RuntimeError(err)
				}
				if addConstraints {
					data.Constraints, _ = addPromptConstraintBuilder(reader, out)
				}
			}

			// Output
			if outputPath == "" {
				outputPath, appendMode, err = promptOutputLocation(reader, out, workdir, manifests, "ConnectionSecret", data.Name)
				if err != nil {
					if errors.Is(err, errAddCanceled) {
						fmt.Fprintln(out, "\nCancelled.")
						return nil
					}
					return RuntimeError(err)
				}
			}

			// Preview
			manifest := RenderConnectionSecretManifest(data)
			fmt.Fprintln(out)
			fmt.Fprintln(out, "=== Preview ===")
			fmt.Fprintln(out, manifest)
			fmt.Fprintln(out, "===============")

			confirmed, _ := addPromptConfirm(reader, out, "Proceed?", true)
			if !confirmed {
				fmt.Fprintln(out, "\nCancelled.")
				return nil
			}

			if err := writeConnectionSecretManifest(cmd, workdir, data, outputPath, appendMode); err != nil {
				return err
			}

			if flagOpenEditor {
				if editor := getEditor(); editor != "" {
					args := buildEditorArgs(editor, filepath.Join(workdir, outputPath))
					execCmd := exec.Command(args[0], args[1:]...)
					execCmd.Stdin = os.Stdin
					execCmd.Stdout = os.Stdout
					execCmd.Stderr = os.Stderr
					_ = execCmd.Run()
				}
			}

			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVar(&flagType, "type", "", "Secret type (postgres, mysql, s3, gcs, r2, http, azure)")
	cmd.Flags().StringVar(&flagPasswordEnv, "password-env", "", "Environment variable for password (database types)")
	cmd.Flags().StringVar(&flagKeyID, "key-id", "", "Key ID (cloud storage types)")
	cmd.Flags().StringVar(&flagSecretEnv, "secret-env", "", "Environment variable for secret key (cloud storage types)")
	cmd.Flags().StringSliceVar(&flagConstraint, "constraint", nil, "Constraints (repeatable)")
	cmd.Flags().StringVarP(&flagOutput, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&flagAppendTo, "append-to", "", "Append to existing file")
	cmd.Flags().StringVar(&flagDesc, "description", "", "Description text")
	cmd.Flags().BoolVar(&flagNoPrompt, "no-prompt", false, "Non-interactive mode")
	cmd.Flags().BoolVar(&flagOpenEditor, "open-editor", false, "Open in $EDITOR after creation")

	_ = cmd.RegisterFlagCompletionFunc("type", completeConnectionSecretTypes)

	return cmd
}

func completeConnectionSecretTypes(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"postgres\tPostgreSQL database",
		"mysql\tMySQL database",
		"s3\tAWS S3 storage",
		"gcs\tGoogle Cloud Storage",
		"r2\tCloudflare R2 storage",
		"http\tHTTP authentication",
		"azure\tAzure Blob Storage",
	}, cobra.ShellCompDirectiveNoFileComp
}

func promptConnectionSecretDetails(reader *bufio.Reader, out interface{}, data *ConnectionSecretManifestData) error {
	fmt.Fprintln(out.(interface{ Write(p []byte) (n int, err error) }), "\nCredential Configuration")
	fmt.Fprintln(out.(interface{ Write(p []byte) (n int, err error) }), "For security, use environment variable references for sensitive values.")
	fmt.Fprintln(out.(interface{ Write(p []byte) (n int, err error) }))

	switch data.Type {
	case ConnectionSecretTypePostgres, ConnectionSecretTypeMySQL:
		if data.PasswordFromEnv == "" {
			var err error
			data.PasswordFromEnv, err = addPromptString(reader, out, "Password environment variable name", "DB_PASSWORD")
			if err != nil {
				return err
			}
		}

	case ConnectionSecretTypeS3, ConnectionSecretTypeGCS, ConnectionSecretTypeR2:
		if data.KeyID == "" {
			var err error
			data.KeyID, err = addPromptString(reader, out, "Access Key ID", "")
			if err != nil {
				return err
			}
		}
		if data.SecretEnv == "" {
			defaultEnv := "AWS_SECRET_ACCESS_KEY"
			if data.Type == ConnectionSecretTypeGCS {
				defaultEnv = "GCS_SECRET_KEY"
			} else if data.Type == ConnectionSecretTypeR2 {
				defaultEnv = "R2_SECRET_ACCESS_KEY"
			}
			var err error
			data.SecretEnv, err = addPromptString(reader, out, "Secret key environment variable name", defaultEnv)
			if err != nil {
				return err
			}
		}

	case ConnectionSecretTypeHTTP:
		options := []SelectOption{
			{Label: "Basic Auth", Description: "Username and password"},
			{Label: "Bearer Token", Description: "Authorization header token"},
		}
		idx, err := addPromptSelect(reader, out, "Authentication type", options, 0)
		if err != nil {
			return err
		}

		if idx == 0 {
			data.Username, err = addPromptString(reader, out, "Username", "")
			if err != nil {
				return err
			}
			data.Password, err = addPromptString(reader, out, "Password environment variable name", "HTTP_PASSWORD")
			if err != nil {
				return err
			}
		} else {
			data.BearerToken, err = addPromptString(reader, out, "Bearer token environment variable name", "HTTP_BEARER_TOKEN")
			if err != nil {
				return err
			}
		}

	case ConnectionSecretTypeAzure:
		options := []SelectOption{
			{Label: "Connection String", Description: "Full connection string from Azure portal"},
			{Label: "Account Key", Description: "Storage account access key"},
		}
		idx, err := addPromptSelect(reader, out, "Authentication method", options, 0)
		if err != nil {
			return err
		}

		if idx == 0 {
			data.ConnectionString, err = addPromptString(reader, out, "Connection string environment variable name", "AZURE_STORAGE_CONNECTION_STRING")
			if err != nil {
				return err
			}
		} else {
			data.AccountKey, err = addPromptString(reader, out, "Account key environment variable name", "AZURE_STORAGE_KEY")
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func writeConnectionSecretManifest(cmd *cobra.Command, workdir string, data ConnectionSecretManifestData, outputPath string, appendMode bool) error {
	out := cmd.OutOrStdout()

	if err := ValidateName(data.Name); err != nil {
		return ConfigError(err)
	}

	manifest := RenderConnectionSecretManifest(data)

	absPath := outputPath
	if !filepath.IsAbs(outputPath) {
		absPath = filepath.Join(workdir, outputPath)
	}

	if appendMode {
		if err := AppendToManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Appended to %s\n", outputPath)
	} else {
		if err := WriteManifest(absPath, manifest); err != nil {
			return RuntimeError(err)
		}
		fmt.Fprintf(out, "Created %s\n", outputPath)
	}

	return nil
}

// RenderConnectionSecretManifest renders a ConnectionSecret manifest from the given data.
func RenderConnectionSecretManifest(data ConnectionSecretManifestData) string {
	var b strings.Builder

	b.WriteString("apiVersion: bino.bi/v1alpha1\n")
	b.WriteString("kind: ConnectionSecret\n")
	b.WriteString("metadata:\n")
	b.WriteString(fmt.Sprintf("  name: %s\n", data.Name))

	if data.Description != "" {
		b.WriteString(fmt.Sprintf("  description: %s\n", quoteYAMLIfNeeded(data.Description)))
	}

	if len(data.Constraints) > 0 {
		b.WriteString("  constraints:\n")
		for _, c := range data.Constraints {
			b.WriteString(fmt.Sprintf("    - %s\n", quoteYAMLIfNeeded(c)))
		}
	}

	b.WriteString("spec:\n")
	b.WriteString(fmt.Sprintf("  type: %s\n", data.Type.TypeString()))

	switch data.Type {
	case ConnectionSecretTypePostgres, ConnectionSecretTypeMySQL:
		if data.PasswordFromEnv != "" {
			b.WriteString(fmt.Sprintf("  passwordFromEnv: %s\n", data.PasswordFromEnv))
		}

	case ConnectionSecretTypeS3, ConnectionSecretTypeGCS, ConnectionSecretTypeR2:
		if data.KeyID != "" {
			b.WriteString(fmt.Sprintf("  keyId: %s\n", data.KeyID))
		}
		if data.SecretEnv != "" {
			b.WriteString(fmt.Sprintf("  secretFromEnv: %s\n", data.SecretEnv))
		}

	case ConnectionSecretTypeHTTP:
		if data.Username != "" {
			b.WriteString(fmt.Sprintf("  username: %s\n", quoteYAMLIfNeeded(data.Username)))
		}
		if data.Password != "" {
			b.WriteString(fmt.Sprintf("  passwordFromEnv: %s\n", data.Password))
		}
		if data.BearerToken != "" {
			b.WriteString(fmt.Sprintf("  bearerTokenFromEnv: %s\n", data.BearerToken))
		}

	case ConnectionSecretTypeAzure:
		if data.ConnectionString != "" {
			b.WriteString(fmt.Sprintf("  connectionStringFromEnv: %s\n", data.ConnectionString))
		}
		if data.AccountKey != "" {
			b.WriteString(fmt.Sprintf("  accountKeyFromEnv: %s\n", data.AccountKey))
		}
	}

	return b.String()
}
