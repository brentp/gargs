package process_test

import (
	"bufio"
	"strings"
	"testing"

	"github.com/brentp/gargs/process"
)

/*
func TestValidCommand(t *testing.T) {

	cmdStr := "go version"

	cmd := process.Run(cmdStr)
	if cmd.Err != nil && cmd.Err != io.EOF {
		t.Fatal(cmd.Err)
	}

	out, err := bufio.NewReader(cmd).ReadString('\n')
	if err != nil {
		t.Errorf("error running command: %s\n%s", cmd, err)
	}

	if !strings.HasPrefix(out, "go ") {
		t.Errorf("error running command: %s", cmd)
	}

	if cmd.ExitCode() != 0 {
		t.Errorf("non-zero exit code %d for command : %s", cmd.ExitCode(), cmd)
	}
}
*/

func TestInvalidCommand(t *testing.T) {

	cmdStr := "XXXXXX go version"

	cmd := process.Run(cmdStr)
	if cmd.Err == nil {
		t.Fatalf("expected error with cmd %s", cmd.Err)
	}

	if cmd.ExitCode() == 0 {
		t.Errorf("zero exit code for bad command : %s", cmd)
	}
}

func TestProcessor(t *testing.T) {

	cmd := make(chan string)
	go func() {
		cmd <- "go version"
		cmd <- "go list"
		close(cmd)
	}()

	k := 0
	for proc := range process.Runner(cmd) {

		out, err := bufio.NewReader(proc).ReadString('\n')
		if err != nil {
			t.Errorf("error running command: %s\n%s", cmd, err)
		}

		if k == 0 && !strings.HasPrefix(out, "go ") {
			t.Errorf("error running command: %s", out)
		}
		if k == 1 && len(out) == 0 {
			t.Errorf("error running command: %s", out)
		}

		if proc.ExitCode() != 0 {
			t.Errorf("non-zero exit code %d for command : %s", proc.ExitCode, cmd)
		}
		k++
	}

}
