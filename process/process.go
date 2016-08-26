package process

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// BufferSize determines how much output will be read into memory before resorting to using a temporary file
const BufferSize = 1048576

// UnknownExit is used when the return/exit-code of the command is not known.
const UnknownExit = 1

func getShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}
	return shell
}

// Command contains a buffered reader with the realized stdout of the process along with the exit code.
type Command struct {
	*bufio.Reader
	tmp      *os.File
	Err      error
	cmd      string
	Duration time.Duration
}

func (c *Command) error() string {
	if c == nil || c.Err == nil {
		return ""
	}
	return c.Err.Error()
}

// Close the temp file associated with the command
func (c *Command) Close() error {
	if c.tmp == nil {
		return nil
	}
	return c.tmp.Close()
}

// Cleanup makes sure the tempfile is closed an deleted.
func (c *Command) Cleanup() {
	if c.tmp != nil {
		c.Close()
		cleanup(c)
	}
}

// String returns a representation of the command that includes run-time, error (if any) and the first 20 chars of stdout.
func (c *Command) String() string {
	cmd := c.cmd
	if len(c.cmd) > 100 {
		cmd = cmd[:80] + "..."
	}
	out, _ := c.Peek(20)
	prompt := ", stdout[:20]: "
	if len(out) < 20 {
		prompt = "stdout: "
	}
	prompt += fmt.Sprintf("'%s'", strings.Replace(string(out), "\n", "\\n", -1))
	errString := ""
	if e := c.error(); e != "" {
		errString = fmt.Sprintf(", error: %s", e)
	}
	exString := ""
	if ex := c.ExitCode(); ex != 0 {
		exString = fmt.Sprintf(", exit-code: %d", ex)
	}

	return fmt.Sprintf("Command('%s', %s%s%s, run-time: %s)",
		cmd, prompt, exString, errString, c.Duration)
}

// ExitCode returns the exit code associated with a given error
func (c *Command) ExitCode() int {
	if c.Err == nil {
		return 0
	}
	if ex, ok := c.Err.(*exec.ExitError); ok {
		if st, ok := ex.Sys().(syscall.WaitStatus); ok {
			return st.ExitStatus()
		}
	}
	return UnknownExit
}

func cleanup(c *Command) {
	c.tmp.Close()
	os.Remove(c.tmp.Name())
}

func newCommand(rdr *bufio.Reader, tmp *os.File, cmd string, err error) *Command {
	c := &Command{rdr, tmp, err, cmd, 0}
	if tmp != nil {
		runtime.SetFinalizer(c, cleanup)
	}
	return c
}

var prefix = fmt.Sprintf("gargs.%d.", os.Getpid())

// Run takes a command string, executes the command,
// Blocks until the output is finished and returns a *Command
// that is an io.Reader. If retries > 0 it will retry on a
// non-zero exit-code.
func Run(command string, retries int) *Command {
	t := time.Now()
	c := oneRun(command)
	for retries > 0 && c.ExitCode() != 0 {
		retries--
		c = oneRun(command)
	}
	c.Duration = time.Since(t)
	return c
}

func oneRun(command string) *Command {

	cmd := exec.Command(getShell(), "-c", command)

	opipe, err := cmd.StdoutPipe()
	if err != nil {
		return newCommand(nil, nil, command, err)
	}
	defer opipe.Close()

	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		return newCommand(nil, nil, command, err)
	}

	bpipe := bufio.NewReaderSize(opipe, BufferSize)

	var res []byte
	res, err = bpipe.Peek(BufferSize)

	// less than BufferSize bytes in output...
	if err == bufio.ErrBufferFull || err == io.EOF {
		err = cmd.Wait()
		return newCommand(bufio.NewReader(bytes.NewReader(res)), nil, command, err)
	}
	if err != nil {
		return newCommand(nil, nil, command, err)
	}

	// more than BufferSize bytes in output. must use tmpfile
	var tmp *os.File
	tmp, err = ioutil.TempFile("", prefix)
	if err != nil {
		return newCommand(bufio.NewReader(bytes.NewReader(res)), tmp, command, err)
	}
	btmp := bufio.NewWriter(tmp)
	_, err = io.CopyBuffer(btmp, bpipe, res)
	if err != nil {
		return newCommand(bufio.NewReader(bytes.NewReader(res)), tmp, command, err)
	}
	opipe.Close()
	btmp.Flush()
	_, err = tmp.Seek(0, 0)
	if err == nil {
		err = cmd.Wait()
	}
	return newCommand(bufio.NewReader(tmp), tmp, command, err)
}

// Runner accepts commands from a channel and sends a bufio.Reader on the returned channel.
// done allows the caller to stop Runner, for example if an error occurs.
// It will parallelize according to GOMAXPROCS.
func Runner(commands <-chan string, retries int, cancel <-chan bool) chan *Command {

	stdout := make(chan *Command, runtime.GOMAXPROCS(0))

	wg := &sync.WaitGroup{}
	wg.Add(runtime.GOMAXPROCS(0))

	// Start a number of workers equal to the requested procs.
	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		go func() {
			defer wg.Done()
			// workers read off the same channel of incoming commands.
			for cmdStr := range commands {
				select {
				case stdout <- Run(cmdStr, retries):
				// if we receive from this, we must exit.
				// receive from closed channel will continually yield false
				// so it does what we expect.
				case <-cancel:
					close(stdout)
					return
				}
			}
		}()
	}

	// wait for all the workers to finish.
	go func() {
		wg.Wait()
		close(stdout)
	}()

	return stdout
}
