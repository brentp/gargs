package process_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
	"strings"
	"testing"

	"github.com/brentp/gargs/process"
)

func TestLongOutput(t *testing.T) {
	// make sure we we test the buffer output.
	cmdStr := "seq 999999"
	cmd := process.Run(cmdStr, nil, nil)
	if cmd.Err != nil {
		t.Fatal(cmd.Err)
	}

}

func TestSigPipe(t *testing.T) {
	// make sure we we test the buffer output.
	cmdStr := "seq 999999 | head"
	cmd := process.Run(cmdStr, nil, &process.Options{Retries: 1})
	if cmd.Err != nil {
		t.Fatal(cmd.Err)
	}
}

func TestEnv(t *testing.T) {
	cmdStr := "echo -n $ZZZ"
	cmd := process.Run(cmdStr, nil, &process.Options{Retries: 1}, "ZZZ=HELLOWORLD")
	if cmd.Err != nil {
		t.Fatal(cmd.Err)
	}

	out, err := ioutil.ReadAll(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "HELLOWORLD" {
		t.Fatalf("expected: HELLOWORLD, got '%s'\n", string(out))
	}

}

func TestCallBack(t *testing.T) {
	// make sure we we test the buffer output.
	cmdStr := "seq 99"
	callback := func(r io.Reader, w io.WriteCloser) error {
		b := bufio.NewReader(r)
		var v, sum int
		for {
			line, err := b.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
			v, err = strconv.Atoi(strings.TrimSpace(line))
			if err != nil {
				return err
			}
			sum += v
		}
		_, err := w.Write([]byte(strconv.Itoa(sum)))
		w.Close()
		return err
	}

	cmd := process.Run(cmdStr, nil, &process.Options{Retries: 1, CallBack: callback})
	if cmd.Err != nil {
		t.Fatal(cmd.Err)
	}
	out, err := bufio.NewReader(cmd).ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if out != "4950" {
		t.Fatalf("expected: 4950, got: %s\n", out)
	}
}

func TestCallBackError(t *testing.T) {
	// make sure we we test the buffer output.
	cmdStr := "seq 99"
	callback := func(r io.Reader, w io.WriteCloser) error {
		w.Write([]byte("22\n"))
		w.Close()
		return errors.New("WE MADE AN ERROR")
	}

	cmd := process.Run(cmdStr, nil, &process.Options{Retries: 1, CallBack: callback})
	if cmd.Err == nil {
		t.Fatal("expected an error")
	}
}

func TestValidCommand(t *testing.T) {

	cmdStr := "go version"

	cmd := process.Run(cmdStr, nil, &process.Options{Retries: 1})
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

func TestInvalidCommand(t *testing.T) {

	cmdStr := "XXXXXX go version"

	cmd := process.Run(cmdStr, nil, nil)
	if cmd.Err == nil {
		t.Fatalf("expected error with cmd %s", cmd.Err)
	}

	if cmd.ExitCode() == 0 {
		t.Errorf("zero exit code for bad command : %s", cmd)
	}
}

func TestProcessIEnv(t *testing.T) {

	o := []bool{true, false}
	for _, ordered := range o {
		opts := process.Options{Retries: 0, Ordered: ordered}
		cmd := make(chan string)
		done := make(chan bool)
		N := 20
		defer close(done)
		go func() {
			for i := 0; i < N; i++ {
				cmd <- "set -u; echo -n $PROCESS_I"
			}
			close(cmd)
		}()
		found := make(map[string]bool, N)
		for proc := range process.Runner(cmd, done, &opts) {
			l, _, err := proc.ReadLine()
			if proc.ExitCode() != 0 {
				t.Fatalf("got non-zero exitcode for %s with opts: %r\n", proc, opts)
			}
			if err != nil && err != io.EOF {
				t.Fatalf("got error: %s for %s\n", err, proc)
			}
			found[string(l)] = true
		}
		for i := 0; i < N; i++ {
			if _, ok := found[strconv.Itoa(i)]; !ok {
				t.Fatalf("didn't find %d in output for PROCESS_I with opts: %r\n", i, opts)
			}
		}
	}
}

func TestOrderedProcessor(t *testing.T) {

	N := 200
	cmd := make(chan string)
	var expected []string
	for i := 0; i < N; i++ {
		expected = append(expected, fmt.Sprintf("%d", i))
	}
	go func() {
		for i := 0; i < N; i++ {
			cmd <- fmt.Sprintf("echo %d", i)
		}
		close(cmd)
	}()
	expectedStr := strings.Join(expected, "\n") + "\n"

	done := make(chan bool)
	defer close(done)
	var got []string

	opts := process.Options{Retries: 0, Ordered: true}
	for proc := range process.Runner(cmd, done, &opts) {

		out, err := ioutil.ReadAll(proc)
		if err != nil {
			t.Errorf("error running command: %s\n%s", cmd, err)
		}
		got = append(got, string(out))
	}
	if strings.Join(got, "") != expectedStr {
		t.Errorf("expected: '%s', got: '%s'", expectedStr, strings.Join(got, ""))
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

	done := make(chan bool)
	defer close(done)
	opts := process.Options{Retries: 0, Ordered: false}
	for proc := range process.Runner(cmd, done, &opts) {

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

func TestLongRunnerError(t *testing.T) {
	// make sure we we test the buffer output.
	cmds := make(chan string)
	go func() {
		cmds <- "seq 999999"
		cmds <- "exit 61"
		cmds <- "sleep 0.5"
		close(cmds)
	}()
	process.BufferSize = 10

	done := make(chan bool)
	defer close(done)
	codes := make([]int, 0, 3)
	opts := process.Options{Retries: 0, Ordered: false}
	for o := range process.Runner(cmds, done, &opts) {
		codes = append(codes, o.ExitCode())
	}
	if codes[0] != 61 && codes[1] != 61 && codes[2] != 61 {
		t.Fatal("expected an exit code of 61")
	}
	if codes[0] != 0 && codes[1] != 0 && codes[2] != 0 {
		t.Fatal("expected an exit code of 0")
	}

}
