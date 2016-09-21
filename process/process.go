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
var BufferSize = 1048576

// UnknownExit is used when the return/exit-code of the command is not known.
const UnknownExit = 1

// prefix for tmp files.
var prefix = fmt.Sprintf("gargs.%d.", os.Getpid())

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
	CmdStr   string
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

// String returns a representation of the command that includes run-time, error (if any) and the first 20 chars of stdout.
func (c *Command) String() string {
	cmd := c.CmdStr
	if len(c.CmdStr) > 100 {
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

// Cleanup makes sure the tempfile is closed an deleted.
func (c *Command) Cleanup() {
	if c.tmp != nil {
		c.Close()
		cleanup(c)
	}
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

// CallBack is an optional function the user can provide to process the
// stdout stream of the called Command. The user is responsible for closing
// the io.Writer
type CallBack func(io.Reader, io.WriteCloser) error

// Run takes a command string, executes the command,
// Blocks until the output is finished and returns a *Command
// that is an io.Reader. If retries > 0 it will retry on a
// non-zero exit-code. If callback is non-nil, it will be executed
// on the stream as it runs.
func Run(command string, retries int, callback CallBack) *Command {
	t := time.Now()
	c := oneRun(command, callback)
	for retries > 0 && c.ExitCode() != 0 {
		retries--
		c = oneRun(command, callback)
	}
	c.Duration = time.Since(t)
	return c
}

func iRun(command istring, retries int, callback CallBack) {
	cmd := Run(command.string, retries, callback)
	command.ch <- cmd
	close(command.ch)
}

func oneRun(command string, callback CallBack) *Command {

	cmd := exec.Command(getShell(), "-c", command)
	var opipe io.Reader

	spipe, err := cmd.StdoutPipe()
	if err != nil {
		return newCommand(nil, nil, command, err)
	}
	defer spipe.Close()
	var errch chan error
	if callback != nil {
		errch = make(chan error, 1)
		rdr, wtr := io.Pipe()
		go func() {
			err := callback(spipe, wtr)
			if err != nil {
				errch <- err
			}
			close(errch)
		}()

		opipe = rdr
	} else {
		opipe = spipe
	}
	if err != nil {
		return newCommand(nil, nil, command, err)
	}

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
		if err == nil && callback != nil {
			if e, ok := <-errch; ok {
				err = e
			}
		}
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
	if c, ok := opipe.(io.ReadCloser); ok {
		c.Close()
	}
	btmp.Flush()
	_, err = tmp.Seek(0, 0)
	if err == nil {
		err = cmd.Wait()
	}
	if err == nil && callback != nil {
		if e, ok := <-errch; ok {
			err = e
		}
	}
	return newCommand(bufio.NewReader(tmp), tmp, command, err)
}

// istring holds a command and an index.
type istring struct {
	string
	ch chan *Command
}

func enumerate(commands <-chan string, istdout chan chan *Command) chan istring {
	ch := make(chan istring)
	go func() {
		for c := range commands {
			cmdch := make(chan *Command, 1)
			istdout <- cmdch
			ch <- istring{c, cmdch}
		}
		close(ch)
		close(istdout)
	}()
	return ch
}

// Runner accepts commands from a channel and sends a bufio.Reader on the returned channel.
// done allows the caller to stop Runner, for example if an error occurs.
// It will parallelize according to GOMAXPROCS. If a callback is specified, the user must
// close the io.Writer before the function returns.
// If ordered is true, the the output will be kept in the order of the input, potentially
// at some cost to the efficiency of parallelization.
func Runner(commands <-chan string, retries int, cancel <-chan bool, callback CallBack, ordered bool) chan *Command {
	if ordered {
		return oRunner(commands, retries, cancel, callback)
	}

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
				case stdout <- Run(cmdStr, retries, callback):
				case <-cancel:
					// if we receive from this, we must exit.
					// receive from closed channel will continually yield false
					// so it does what we expect.
					close(stdout)
					break
				}

			}
		}()
	}

	go func() {
		wg.Wait()
		close(stdout)
	}()

	return stdout
}

// use separate runner when they want output in order of input. this
// uses istdout and a channel of channels where a channel gets pushed oneRun
// in the order of input and that same channel gets pushed to when they
// command is finished.
func oRunner(commands <-chan string, retries int, cancel <-chan bool, callback CallBack) chan *Command {

	stdout := make(chan *Command, runtime.GOMAXPROCS(0))

	istdout := make(chan chan *Command, 3*runtime.GOMAXPROCS(0))
	icommands := enumerate(commands, istdout)

	// Start a number of workers equal to the requested procs.
	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		go func() {
			// workers read off the same channel of incoming commands.
			for cmd := range icommands {
				iRun(cmd, retries, callback)
			}
		}()
	}

	go func() {
		for ch := range istdout {
			select {

			case stdout <- <-ch:
			case <-cancel:
				break
			}
		}
		close(stdout)
	}()

	return stdout
}
