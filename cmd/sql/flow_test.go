package sql

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	sql "github.com/astronomer/astro-cli/sql"
	"github.com/astronomer/astro-cli/sql/mocks"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var (
	errMock            = errors.New("mock error")
	imageBuildResponse = types.ImageBuildResponse{
		Body: io.NopCloser(strings.NewReader("Image built")),
	}
	containerCreateCreatedBody          = container.ContainerCreateCreatedBody{ID: "123"}
	sampleLog                           = io.NopCloser(strings.NewReader("Sample log"))
	mockExecuteCmdInDockerReturnSuccess = func(cmd, args []string, flags map[string]string, mountDirs []string, returnOutput bool) (exitCode int64, output io.ReadCloser, err error) {
		return 0, output, nil
	}
	mockExecuteCmdInDockerReturnErr = func(cmd, args []string, flags map[string]string, mountDirs []string, returnOutput bool) (exitCode int64, output io.ReadCloser, err error) {
		return 0, output, errMock
	}
	mockExecuteCmdInDockerReturnNonZeroExitCode = func(cmd, args []string, flags map[string]string, mountDirs []string, returnOutput bool) (exitCode int64, output io.ReadCloser, err error) {
		return 1, output, nil
	}
	mockConvertReadCloserToStringReturnErr = func(readCloser io.ReadCloser) (string, error) {
		return "", errMock
	}
	mockAppendConfigKeyMountDirErr = func(configKey string, configFlags map[string]string, mountDirs []string) ([]string, error) {
		return nil, errMock
	}
)

func getContainerWaitResponse(raiseError bool, statusCode int64) (bodyCh <-chan container.ContainerWaitOKBody, errCh <-chan error) {
	containerWaitOkBodyChannel := make(chan container.ContainerWaitOKBody)
	errChannel := make(chan error, 1)
	go func() {
		if raiseError {
			errChannel <- errMock
			return
		}
		res := container.ContainerWaitOKBody{StatusCode: statusCode}
		containerWaitOkBodyChannel <- res
		errChannel <- nil
		close(containerWaitOkBodyChannel)
		close(errChannel)
	}()
	// converting bidirectional channel to read only channels for ContainerWait to consume
	var readOnlyStatusCh <-chan container.ContainerWaitOKBody
	var readOnlyErrCh <-chan error
	readOnlyStatusCh = containerWaitOkBodyChannel
	readOnlyErrCh = errChannel
	return readOnlyStatusCh, readOnlyErrCh
}

// patches ExecuteCmdInDocker and
// returns a function that, when called, restores the original values.
func patchExecuteCmdInDocker(t *testing.T, statusCode int64, err error) func() {
	mockDocker := mocks.NewDockerBind(t)
	sql.Docker = func() (sql.DockerBind, error) {
		if err == nil {
			mockDocker.On("ImageBuild", mock.Anything, mock.Anything, mock.Anything).Return(imageBuildResponse, nil)
			mockDocker.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(containerCreateCreatedBody, nil)
			mockDocker.On("ContainerStart", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			mockDocker.On("ContainerWait", mock.Anything, mock.Anything, mock.Anything).Return(getContainerWaitResponse(false, statusCode))
			mockDocker.On("ContainerLogs", mock.Anything, mock.Anything, mock.Anything).Return(sampleLog, nil)
			mockDocker.On("ContainerRemove", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		}
		return mockDocker, err
	}
	sql.DisplayMessages = func(r io.Reader) error {
		return nil
	}
	mockIo := mocks.NewIoBind(t)
	sql.Io = func() sql.IoBind {
		mockIo.On("Copy", mock.Anything, mock.Anything).Return(int64(0), nil)
		return mockIo
	}

	return func() {
		sql.Docker = sql.NewDockerBind
		sql.Io = sql.NewIoBind
		sql.DisplayMessages = sql.OriginalDisplayMessages
	}
}

// chdir changes the current working directory to the named directory and
// returns a function that, when called, restores the original working
// directory.
func chdir(t *testing.T, dir string) func() {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	return func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restoring working directory: %v", err)
		}
	}
}

func execFlowCmd(args ...string) error {
	cmd := NewFlowCommand()
	cmd.SetArgs(args)
	_, err := cmd.ExecuteC()
	return err
}

func TestFlowCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	err := execFlowCmd()
	assert.NoError(t, err)
}

func TestFlowCmdError(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, errMock)()
	err := execFlowCmd("version")
	assert.EqualError(t, err, "error running [version]: docker client initialization failed mock error")
}

func TestFlowCmdHelpError(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, errMock)()
	assert.PanicsWithError(t, "error running []: docker client initialization failed mock error", func() { execFlowCmd() })
}

func TestFlowCmdDockerCommandError(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 1, nil)()
	err := execFlowCmd("version")
	assert.EqualError(t, err, "docker command has returned a non-zero exit code:1")
}

func TestFlowCmdDockerCommandHelpError(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 1, nil)()
	assert.PanicsWithError(t, "docker command has returned a non-zero exit code:1", func() { execFlowCmd() })
}

func TestFlowVersionCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	err := execFlowCmd("version")
	assert.NoError(t, err)
}

func TestFlowAboutCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	err := execFlowCmd("about")
	assert.NoError(t, err)
}

func TestFlowInitCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	defer chdir(t, projectDir)()
	err := execFlowCmd("init")
	assert.NoError(t, err)
}

func TestFlowInitCmdWithFlags(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	AirflowHome := t.TempDir()
	AirflowDagsFolder := t.TempDir()
	DataDir := t.TempDir()
	err := execFlowCmd("init", projectDir, "--airflow-home", AirflowHome, "--airflow-dags-folder", AirflowDagsFolder, "--data-dir", DataDir)
	assert.NoError(t, err)
}

func TestFlowConfigCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()

	testCases := []struct {
		initFlag  string
		configKey string
	}{
		{"--airflow-home", "airflow_home"},
		{"--airflow-dags-folder", "airflow_dags_folder"},
		{"--data-dir", "data_dir"},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("with init flag %s and config key %s", tc.initFlag, tc.configKey), func(t *testing.T) {
			projectDir := t.TempDir()
			err := execFlowCmd("init", projectDir, tc.initFlag, t.TempDir())
			assert.NoError(t, err)

			err = execFlowCmd("config", "--project-dir", projectDir, tc.configKey)
			assert.NoError(t, err)
		})
	}
}

func TestFlowConfigCmdArgumentNotSetError(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	err := execFlowCmd("init", projectDir)
	assert.NoError(t, err)

	err = execFlowCmd("config", "--project-dir", projectDir)
	assert.EqualError(t, err, "argument not set:key")
}

func TestFlowValidateCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	err := execFlowCmd("init", projectDir)
	assert.NoError(t, err)

	err = execFlowCmd("validate", projectDir, "--connection", "sqlite_conn", "--verbose")
	assert.NoError(t, err)
}

func TestFlowGenerateCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	err := execFlowCmd("init", projectDir)
	assert.NoError(t, err)

	err = execFlowCmd("generate", "example_basic_transform", "--project-dir", projectDir, "--no-generate-tasks", "--verbose")
	assert.NoError(t, err)
}

func TestFlowGenerateGenerateTasksCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	err := execFlowCmd("init", projectDir)
	assert.NoError(t, err)

	err = execFlowCmd("generate", "example_basic_transform", "--project-dir", projectDir, "--generate-tasks")
	assert.NoError(t, err)
}

func TestFlowRunGenerateTasksCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	err := execFlowCmd("init", projectDir)
	assert.NoError(t, err)

	err = execFlowCmd("run", "example_basic_transform", "--project-dir", projectDir, "--generate-tasks")
	assert.NoError(t, err)
}

func TestFlowGenerateCmdWorkflowNameNotSet(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	err := execFlowCmd("init", projectDir)
	assert.NoError(t, err)

	err = execFlowCmd("generate", "--project-dir", projectDir)
	assert.EqualError(t, err, "argument not set:workflow_name")
}

func TestFlowRunCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	err := execFlowCmd("init", projectDir)
	assert.NoError(t, err)

	err = execFlowCmd("run", "example_templating", "--env", "dev", "--project-dir", projectDir, "--no-generate-tasks", "--verbose")
	assert.NoError(t, err)
}

func TestDebugFlowRunCmd(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	err := execFlowCmd("init", projectDir)
	assert.NoError(t, err)

	err = execFlowCmd("--debug", "run", "example_templating", "--env", "dev", "--project-dir", projectDir)
	assert.NoError(t, err)
}

func TestFlowRunCmdWorkflowNameNotSet(t *testing.T) {
	defer patchExecuteCmdInDocker(t, 0, nil)()
	projectDir := t.TempDir()
	err := execFlowCmd("init", projectDir)
	assert.NoError(t, err)

	err = execFlowCmd("run", "--project-dir", projectDir)
	assert.EqualError(t, err, "argument not set:workflow_name")
}

func TestAppendConfigKeyMountDirInvalidCommand(t *testing.T) {
	originalDockerUtil := sql.ExecuteCmdInDocker
	originalConvertReadCloserToString := sql.ConvertReadCloserToString

	sql.ExecuteCmdInDocker = mockExecuteCmdInDockerReturnErr
	_, err := appendConfigKeyMountDir("", nil, nil)
	expectedErr := fmt.Errorf("error running %v: %w", configCommandString, errMock)
	assert.Equal(t, expectedErr, err)

	sql.ExecuteCmdInDocker = originalDockerUtil
	sql.ConvertReadCloserToString = originalConvertReadCloserToString
}

func TestAppendConfigKeyMountDirDockerNonZeroExitCodeError(t *testing.T) {
	originalDockerUtil := sql.ExecuteCmdInDocker
	originalConvertReadCloserToString := sql.ConvertReadCloserToString

	sql.ExecuteCmdInDocker = mockExecuteCmdInDockerReturnNonZeroExitCode
	_, err := appendConfigKeyMountDir("", nil, nil)
	expectedErr := sql.DockerNonZeroExitCodeError(1)
	assert.Equal(t, expectedErr, err)

	sql.ExecuteCmdInDocker = originalDockerUtil
	sql.ConvertReadCloserToString = originalConvertReadCloserToString
}

func TestAppendConfigKeyMountDirReadError(t *testing.T) {
	originalDockerUtil := sql.ExecuteCmdInDocker
	originalConvertReadCloserToString := sql.ConvertReadCloserToString

	sql.ExecuteCmdInDocker = mockExecuteCmdInDockerReturnSuccess
	sql.ConvertReadCloserToString = mockConvertReadCloserToStringReturnErr
	_, err := appendConfigKeyMountDir("", nil, nil)
	assert.EqualError(t, err, "mock error")

	sql.ExecuteCmdInDocker = originalDockerUtil
	sql.ConvertReadCloserToString = originalConvertReadCloserToString
}

func TestBuildFlagsAndMountDirsFailures(t *testing.T) {
	originalAppendConfigKeyMountDir := appendConfigKeyMountDir

	appendConfigKeyMountDir = mockAppendConfigKeyMountDirErr
	_, _, err := buildFlagsAndMountDirs("", false, false, false, false, true)
	assert.EqualError(t, err, "mock error")

	appendConfigKeyMountDir = originalAppendConfigKeyMountDir
}
