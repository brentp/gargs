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
	tmpName string
	Err     error
	cmd     string
}

func (c *Command) error() string {
	if c == nil || c.Err == nil {
		return ""
	}
	return c.Err.Error()
}

func (c *Command) String() string {
	cmd := c.cmd
	if len(c.cmd) > 100 {
		cmd = cmd[:80] + "..."
	}
	out, _ := c.Peek(20)
	return fmt.Sprintf("Command('%s', output[:20]: %s, exit-code: %d, error: %s)",
		cmd, strings.Replace(string(out), "\n", "\\n", -1), c.ExitCode(), c.error())
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
	os.Remove(c.tmpName)
}

func newCommand(rdr *bufio.Reader, tmpName string, cmd string, err error) *Command {
	c := &Command{rdr, tmpName, err, cmd}
	if tmpName != "" {
		runtime.SetFinalizer(c, cleanup)
	}
	return c
}

// Run takes a command string, executes the command,
// Blocks until the output is finished and returns a *Command
// that is an io.Reader
func Run(command string, stderr ...io.Writer) *Command {

	cmd := exec.Command(getShell(), "-c", command)

	opipe, err := cmd.StdoutPipe()
	if err != nil {
		return newCommand(nil, "", command, err)
	}
	defer opipe.Close()

	if len(stderr) != 0 {
		cmd.Stderr = stderr[0]
	} else {
		cmd.Stderr = os.Stderr
	}

	err = cmd.Start()
	if err != nil {
		return newCommand(nil, "", command, err)
	}

	bpipe := bufio.NewReaderSize(opipe, BufferSize)
	var res []byte
	res, err = bpipe.Peek(BufferSize)

	// less than BufferSize bytes in output...
	if err == bufio.ErrBufferFull || err == io.EOF {
		err = cmd.Wait()
		return newCommand(bufio.NewReader(bytes.NewReader(res)), "", command, err)
	}
	if err != nil {
		return newCommand(nil, "", command, err)
	}

	// more than BufferSize bytes in output. must use tmpfile
	var tmp *os.File
	tmp, err = ioutil.TempFile("", "gargsTmp.")
	if err != nil {
		return newCommand(bufio.NewReader(bytes.NewReader(res)), "", command, err)
	}
	btmp := bufio.NewWriter(tmp)
	_, err = io.CopyBuffer(btmp, bpipe, res)
	if err != nil {
		return newCommand(bufio.NewReader(bytes.NewReader(res)), "", command, err)
	}
	err = opipe.Close()
	btmp.Flush()
	_, err = tmp.Seek(0, 0)
	return newCommand(bufio.NewReader(tmp), tmp.Name(), command, err)
}

// Runner accepts commands from a channel and sends a bufio.Reader on the returned channel.
// done allows the caller to stop Runner, for example if an error occurs.
// It will parallelize according to GOMAXPROCS.
func Runner(commands <-chan string, done <-chan bool, stderr ...io.Writer) chan *Command {

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
				case stdout <- Run(cmdStr, stderr...):
				// if we receive from this, we must exit.
				// receive from closed channel will continually yield false
				// so it does what we expect.
				case <-done:
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
