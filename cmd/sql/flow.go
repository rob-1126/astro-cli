package sql

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/astronomer/astro-cli/sql"
	"github.com/spf13/cobra"
)

var (
	environment       string
	connection        string
	airflowHome       string
	airflowDagsFolder string
	dataDir           string
	projectDir        string
	generateTasks     bool
	noGenerateTasks   bool
	verbose           bool
	debug             bool
)

var (
	configCommandString = []string{"config"}
	globalConfigKeys    = []string{"airflow_home", "airflow_dags_folder", "data_dir"}
)

func getAbsolutePath(path string) (string, error) {
	if !filepath.IsAbs(path) || path == "" || path == "." {
		currentDir, err := os.Getwd()
		if err != nil {
			err = fmt.Errorf("error getting current directory %w", err)
			return "", err
		}
		path = filepath.Join(currentDir, path)
	}
	return path, nil
}

func createProjectDir(projectDir string) (mountDir string, err error) {
	projectDir, err = getAbsolutePath(projectDir)
	if err != nil {
		return "", err
	}

	err = os.MkdirAll(projectDir, os.ModePerm)

	if err != nil {
		err = fmt.Errorf("error creating project directory %s: %w", projectDir, err)
		return "", err
	}

	return projectDir, nil
}

func getBaseMountDirs(projectDir string) ([]string, error) {
	mountDir, err := createProjectDir(projectDir)
	if err != nil {
		return nil, err
	}
	mountDirs := []string{mountDir}
	return mountDirs, nil
}

var appendConfigKeyMountDir = func(configKey string, configFlags map[string]string, mountDirs []string) ([]string, error) {
	args := []string{configKey}
	exitCode, output, err := sql.ExecuteCmdInDocker(configCommandString, args, configFlags, mountDirs, true)
	if err != nil {
		return mountDirs, fmt.Errorf("error running %v: %w", configCommandString, err)
	}
	if exitCode != 0 {
		return mountDirs, sql.DockerNonZeroExitCodeError(exitCode)
	}
	configKeyDir, err := sql.ConvertReadCloserToString(output)
	if err != nil {
		return mountDirs, err
	}
	mountDirs = append(mountDirs, strings.TrimSpace(configKeyDir))
	return mountDirs, nil
}

func buildFlagsAndMountDirs(projectDir string, setProjectDir, setAirflowHome, setAirflowDagsFolder, setDataDir, mountGlobalDirs bool) (flags map[string]string, mountDirs []string, err error) {
	flags = make(map[string]string)
	mountDirs, err = getBaseMountDirs(projectDir)
	if err != nil {
		return nil, nil, err
	}

	if setProjectDir {
		projectDir, err = getAbsolutePath(projectDir)
		if err != nil {
			return nil, nil, err
		}
		flags["project-dir"] = projectDir
	}

	if mountGlobalDirs {
		configFlags := make(map[string]string)
		configFlags["project-dir"] = projectDir
		for _, globalConfigKey := range globalConfigKeys {
			mountDirs, err = appendConfigKeyMountDir(globalConfigKey, configFlags, mountDirs)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	if setAirflowHome && airflowHome != "" {
		airflowHomeAbs, err := getAbsolutePath(airflowHome)
		if err != nil {
			return nil, nil, err
		}
		flags["airflow-home"] = airflowHomeAbs
		mountDirs = append(mountDirs, airflowHomeAbs)
	}

	if setAirflowDagsFolder && airflowDagsFolder != "" {
		airflowDagsFolderAbs, err := getAbsolutePath(airflowDagsFolder)
		if err != nil {
			return nil, nil, err
		}
		flags["airflow-dags-folder"] = airflowDagsFolderAbs
		mountDirs = append(mountDirs, airflowDagsFolderAbs)
	}

	if setDataDir && dataDir != "" {
		dataDirAbs, err := getAbsolutePath(dataDir)
		if err != nil {
			return nil, nil, err
		}
		flags["data-dir"] = dataDirAbs
		mountDirs = append(mountDirs, dataDirAbs)
	}

	return flags, mountDirs, nil
}

func executeCmd(cmd *cobra.Command, args []string, flags map[string]string, mountDirs []string) error {
	cmdString := []string{cmd.Name()}
	if debug {
		cmdString = []string{"--debug", cmd.Name()}
	}
	exitCode, _, err := sql.ExecuteCmdInDocker(cmdString, args, flags, mountDirs, false)
	if err != nil {
		return fmt.Errorf("error running %v: %w", cmdString, err)
	}
	if exitCode != 0 {
		return sql.DockerNonZeroExitCodeError(exitCode)
	}

	return nil
}

func executeBase(cmd *cobra.Command, args []string) error {
	flags, mountDirs, err := buildFlagsAndMountDirs(projectDir, false, false, false, false, false)
	if err != nil {
		return err
	}
	return executeCmd(cmd, args, flags, mountDirs)
}

func executeInit(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		projectDir = args[0]
	}

	flags, mountDirs, err := buildFlagsAndMountDirs(projectDir, false, true, true, true, false)
	if err != nil {
		return err
	}

	projectDirAbsolute := mountDirs[0]
	args = []string{projectDirAbsolute}

	return executeCmd(cmd, args, flags, mountDirs)
}

func executeConfig(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return sql.ArgNotSetError("key")
	}

	flags, mountDirs, err := buildFlagsAndMountDirs(projectDir, true, false, false, false, false)
	if err != nil {
		return err
	}

	if environment != "" {
		flags["env"] = environment
	}

	return executeCmd(cmd, args, flags, mountDirs)
}

func executeValidate(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		projectDir = args[0]
	}

	flags, mountDirs, err := buildFlagsAndMountDirs(projectDir, false, false, false, false, false)
	if err != nil {
		return err
	}

	projectDirAbsolute := mountDirs[0]
	args = []string{projectDirAbsolute}

	if environment != "" {
		flags["env"] = environment
	}

	if connection != "" {
		flags["connection"] = connection
	}

	if verbose {
		args = append(args, "--verbose")
	}

	return executeCmd(cmd, args, flags, mountDirs)
}

func executeGenerate(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return sql.ArgNotSetError("workflow_name")
	}

	flags, mountDirs, err := buildFlagsAndMountDirs(projectDir, true, false, false, false, true)
	if err != nil {
		return err
	}

	if generateTasks {
		args = append(args, "--generate-tasks")
	}
	if noGenerateTasks {
		args = append(args, "--no-generate-tasks")
	}

	if environment != "" {
		flags["env"] = environment
	}

	if verbose {
		args = append(args, "--verbose")
	}

	return executeCmd(cmd, args, flags, mountDirs)
}

func executeRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return sql.ArgNotSetError("workflow_name")
	}

	flags, mountDirs, err := buildFlagsAndMountDirs(projectDir, true, false, false, false, true)
	if err != nil {
		return err
	}

	if environment != "" {
		flags["env"] = environment
	}

	if verbose {
		args = append(args, "--verbose")
	}

	if generateTasks {
		args = append(args, "--generate-tasks")
	}
	if noGenerateTasks {
		args = append(args, "--no-generate-tasks")
	}

	return executeCmd(cmd, args, flags, mountDirs)
}

func executeHelp(cmd *cobra.Command, cmdString []string) {
	exitCode, _, err := sql.ExecuteCmdInDocker(cmdString, nil, nil, nil, false)
	if err != nil {
		panic(fmt.Errorf("error running %v: %w", cmdString, err))
	}
	if exitCode != 0 {
		panic(sql.DockerNonZeroExitCodeError(exitCode))
	}
}

func aboutCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "about",
		Args:         cobra.MaximumNArgs(1),
		RunE:         executeBase,
		SilenceUsage: true,
	}
	cmd.SetHelpFunc(executeHelp)
	return cmd
}

func versionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "version",
		Args:         cobra.MaximumNArgs(1),
		RunE:         executeBase,
		SilenceUsage: true,
	}
	cmd.SetHelpFunc(executeHelp)
	return cmd
}

func initCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "init",
		Args:         cobra.MaximumNArgs(1),
		RunE:         executeInit,
		SilenceUsage: true,
	}
	cmd.SetHelpFunc(executeHelp)
	cmd.Flags().StringVar(&airflowHome, "airflow-home", "", "")
	cmd.Flags().StringVar(&airflowDagsFolder, "airflow-dags-folder", "", "")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "")
	return cmd
}

func configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "config",
		Args:         cobra.MaximumNArgs(1),
		RunE:         executeConfig,
		SilenceUsage: true,
	}
	cmd.SetHelpFunc(executeHelp)
	cmd.Flags().StringVar(&projectDir, "project-dir", ".", "")
	cmd.Flags().StringVar(&environment, "env", "default", "")
	return cmd
}

func validateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "validate",
		Args:         cobra.MaximumNArgs(1),
		RunE:         executeValidate,
		SilenceUsage: true,
	}
	cmd.SetHelpFunc(executeHelp)
	cmd.Flags().StringVar(&environment, "env", "default", "")
	cmd.Flags().StringVar(&connection, "connection", "", "")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "")
	return cmd
}

//nolint:dupl
func generateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "generate",
		Args:         cobra.MaximumNArgs(1),
		RunE:         executeGenerate,
		SilenceUsage: true,
	}
	cmd.SetHelpFunc(executeHelp)
	cmd.Flags().BoolVar(&generateTasks, "generate-tasks", false, "")
	cmd.Flags().BoolVar(&noGenerateTasks, "no-generate-tasks", false, "")
	cmd.Flags().StringVar(&environment, "env", "default", "")
	cmd.Flags().StringVar(&projectDir, "project-dir", ".", "")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "")
	cmd.MarkFlagsMutuallyExclusive("generate-tasks", "no-generate-tasks")
	return cmd
}

//nolint:dupl
func runCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "run",
		Args:         cobra.MaximumNArgs(1),
		RunE:         executeRun,
		SilenceUsage: true,
	}
	cmd.SetHelpFunc(executeHelp)
	cmd.Flags().BoolVar(&generateTasks, "generate-tasks", false, "")
	cmd.Flags().BoolVar(&noGenerateTasks, "no-generate-tasks", false, "")
	cmd.Flags().StringVar(&environment, "env", "default", "")
	cmd.Flags().StringVar(&projectDir, "project-dir", ".", "")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "")
	cmd.MarkFlagsMutuallyExclusive("generate-tasks", "no-generate-tasks")
	return cmd
}

func login(cmd *cobra.Command, args []string) error {
	// flow currently does not require login
	return nil
}

func NewFlowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "flow",
		Short:             "Run flow commands",
		PersistentPreRunE: login,
		Run:               executeHelp,
		SilenceUsage:      true,
	}
	cmd.SetHelpFunc(executeHelp)
	cmd.PersistentFlags().BoolVar(&debug, "debug", false, "")
	cmd.AddCommand(versionCommand())
	cmd.AddCommand(aboutCommand())
	cmd.AddCommand(initCommand())
	cmd.AddCommand(configCommand())
	cmd.AddCommand(validateCommand())
	cmd.AddCommand(generateCommand())
	cmd.AddCommand(runCommand())
	return cmd
}
