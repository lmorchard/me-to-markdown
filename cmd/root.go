package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/lmorchard/me-to-markdown/internal/config"
	"github.com/lmorchard/me-to-markdown/internal/envfile"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile      string
	log          = logrus.New()
	cfg          *config.Config
	envFileExtra []string // loaded once at startup; merged into subprocess envs
)

var rootCmd = &cobra.Command{
	Use:   appName,
	Short: "Orchestrate *-to-markdown tools into a single combined report",
	Long: `me-to-markdown runs the family of *-to-markdown tools in parallel over a
shared time window and concatenates their output into a single Markdown
document.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initConfig()
		setupLogging()
		loadEnvFile()
	},
	SilenceUsage:  true,
	SilenceErrors: false,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", fmt.Sprintf("config file (default is ./%s.yaml)", configName))
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().Bool("debug", false, "debug output")
	rootCmd.PersistentFlags().Bool("log-json", false, "output logs in JSON format")
	rootCmd.PersistentFlags().String("env-file", "", fmt.Sprintf("KEY=VALUE file merged into each subprocess env (default: %s if present)", envfile.DefaultPath()))

	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("log_json", rootCmd.PersistentFlags().Lookup("log-json"))
	_ = viper.BindPFlag("env_file", rootCmd.PersistentFlags().Lookup("env-file"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(configName)
	}

	viper.SetDefault("verbose", false)
	viper.SetDefault("debug", false)
	viper.SetDefault("log_json", false)

	viper.SetEnvPrefix(envPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if cfgFile != "" {
			fmt.Fprintf(os.Stderr, "Error reading config file: %v\n", err)
			os.Exit(1)
		}
	}
}

func setupLogging() {
	if viper.GetBool("log_json") {
		log.SetFormatter(&logrus.JSONFormatter{})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	}

	switch {
	case viper.GetBool("debug"):
		log.SetLevel(logrus.DebugLevel)
	case viper.GetBool("verbose"):
		log.SetLevel(logrus.InfoLevel)
	default:
		log.SetLevel(logrus.WarnLevel)
	}
}

// GetConfig returns the application configuration, loading it if necessary.
func GetConfig() *config.Config {
	if cfg == nil {
		cfg = &config.Config{
			Verbose:    viper.GetBool("verbose"),
			Debug:      viper.GetBool("debug"),
			LogJSON:    viper.GetBool("log_json"),
			Since:      viper.GetString("since"),
			Include:    viper.GetStringSlice("include"),
			Exclude:    viper.GetStringSlice("exclude"),
			OmitErrors: viper.GetBool("omit_errors"),
			EnvFile:    viper.GetString("env_file"),
		}
	}
	return cfg
}

// ExtraEnv returns the KEY=VALUE entries loaded from the env file, ready
// to append to exec.Cmd.Env. Empty if no env file was loaded.
func ExtraEnv() []string {
	return envFileExtra
}

// loadEnvFile resolves the effective env file path (--env-file flag /
// env_file: config / default) and parses it into envFileExtra. A
// resolved path that doesn't exist is silently ignored; a path the user
// explicitly supplied that fails to parse is fatal.
func loadEnvFile() {
	explicit := viper.GetString("env_file")
	path := explicit
	if path == "" {
		path = envfile.DefaultPath()
	}

	entries, err := envfile.Load(path)
	if err != nil {
		if explicit != "" {
			fmt.Fprintf(os.Stderr, "Error reading env file: %v\n", err)
			os.Exit(1)
		}
		// Implicit default path failed to parse — log and continue.
		log.Warnf("env file %s: %v (ignoring)", path, err)
		return
	}
	envFileExtra = entries
	if len(entries) > 0 {
		log.Debugf("loaded %d entr(y/ies) from env file %s", len(entries), path)
	}
}

// GetLogger returns the configured logger.
func GetLogger() *logrus.Logger {
	return log
}
