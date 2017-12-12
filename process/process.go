package process

import (
	"bufio"
	"bytes"
	"compress/gzip"
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

// WaitingMultiplier determines how many finished processes can be waiting before Runner blocks waiting for
// the slowest process finishes. Increasing this improves concurrency at the expense of memory.
var WaitingMultiplier = 4

// UnknownExit is used when the return/exit-code of the command is not known.
const UnknownExit = 1

// prefix for tmp files.
var prefix = fmt.Sprintf("gargs.%d.", os.Getpid())

func getShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		if _, err := os.Stat("/bin/bash"); err == nil {
			shell = "bash"
		} else {
			shell = "sh"
		}
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
	var prompt string
	if c.Reader != nil {
		out, _ := c.Peek(20)
		prompt = ", stdout[:20]: "
		if len(out) < 20 {
			prompt = "stdout: "
		}
		prompt += fmt.Sprintf("'%s'", strings.Replace(string(out), "\n", "\\n", -1))
	} else {
		prompt = ", [no stdout]"

	}
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
// that is an io.Reader. See Options for additional details.
func Run(command string, opts *Options, env ...string) *Command {
	t := time.Now()
	var c *Command
	var retries int
	if opts == nil {
		c = oneRun(command, nil, env)
	} else {
		c = oneRun(command, opts.CallBack, env)
		retries = opts.Retries
	}
	for retries > 0 && c.ExitCode() != 0 {
		retries--
		c = oneRun(command, opts.CallBack, env)
	}
	c.Duration = time.Since(t)
	return c
}

// oRun calls run and sends result to channel. used when we want
// to keep output in same order as input
func oRun(command istring, opts *Options, env ...string) {
	cmd := Run(command.string, opts, env...)
	command.ch <- cmd
	close(command.ch)
}

func oneRun(command string, callback CallBack, env []string) *Command {

	cmd := exec.Command(getShell(), "-c", command)
	if len(env) > 0 {
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, env...)
	}
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

	gtmp, err := gzip.NewWriterLevel(tmp, gzip.BestSpeed)
	if err != nil {
		return newCommand(bufio.NewReader(bytes.NewReader(res)), tmp, command, err)
	}

	_, err = io.CopyBuffer(gtmp, bpipe, res)
	if err != nil {
		return newCommand(bufio.NewReader(bytes.NewReader(res)), tmp, command, err)
	}
	if c, ok := opipe.(io.ReadCloser); ok {
		c.Close()
	}
	gtmp.Flush()
	gtmp.Close()
	_, err = tmp.Seek(0, 0)
	if err == nil {
		err = cmd.Wait()
	}
	if err == nil && callback != nil {
		if e, ok := <-errch; ok {
			err = e
		}
	}
	var grdr *gzip.Reader
	grdr, err = gzip.NewReader(tmp)
	return newCommand(bufio.NewReader(grdr), tmp, command, err)
}

// istring holds a command and an index.
type istring struct {
	string
	ch chan *Command
	i  int
}

// add the index (i) to a command so we know the order.io
// if istdout is nil, then we only add the index. otherwise, when
// push a channel onto istdout and into each istring to keep
// the order.
func enumerate(commands <-chan string, istdout chan chan *Command) chan istring {
	ch := make(chan istring)
	var cmdch chan *Command
	go func() {
		i := 0
		for c := range commands {
			if istdout != nil {
				cmdch = make(chan *Command)
				istdout <- cmdch
			}
			ch <- istring{c, cmdch, i}
			i++
		}
		close(ch)
		if istdout != nil {
			close(istdout)
		}
	}()
	return ch
}

// Options holds the options to send to Runner.
type Options struct {
	// A callback to be applied to the output of the command. The user is responsible
	// for closing the io.Writer insde the this function.
	CallBack CallBack
	// Ordered keeps the output in order even when the processes finish in a different order.io
	// This can come at the expense of performance when waiting on a long process.
	Ordered bool
	// Retries indicates the number of times a process will be retried if it has
	// a non-zero exit code.
	Retries int
}

// Runner accepts commands from a channel and sends a bufio.Reader on the returned channel.
// done allows the caller to stop Runner, for example if an error occurs.
// It will parallelize according to GOMAXPROCS. See Options for more details.
func Runner(commands <-chan string, cancel <-chan bool, opts *Options) chan *Command {
	if opts.Ordered {
		return oRunner(commands, cancel, opts)
	}

	stdout := make(chan *Command, runtime.GOMAXPROCS(0))
	icommands := enumerate(commands, nil)

	wg := &sync.WaitGroup{}
	wg.Add(runtime.GOMAXPROCS(0))

	// Start a number of workers equal to the requested procs.
	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		go func() {
			defer wg.Done()
			// workers read off the same channel of incoming commands.
			for cmd := range icommands {
				select {
				case stdout <- Run(cmd.string, opts, fmt.Sprintf("PROCESS_I=%d", cmd.i)):
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
func oRunner(commands <-chan string, cancel <-chan bool, opts *Options) chan *Command {

	stdout := make(chan *Command, runtime.GOMAXPROCS(0))

	// this means that if e.g. 12 processors are available and WaitingMultiplier is 4
	// then up to 47 finished processes can be blocked waiting for the slowest one to finish.
	istdout := make(chan chan *Command, WaitingMultiplier*runtime.GOMAXPROCS(0))
	icommands := enumerate(commands, istdout)

	// Start a number of workers equal to the requested procs.
	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		go func() {
			// workers read off the same channel of incoming commands.
			for cmd := range icommands {
				oRun(cmd, opts, fmt.Sprintf("PROCESS_I=%d", cmd.i))
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
