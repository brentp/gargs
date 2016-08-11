package process

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
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

func (c *Command) String() string {
	return c.cmd
}

// ExitCode returns the exit code associated with a given error
func (o *Command) ExitCode() int {
	if o.Err == nil {
		return 0
	}
	if ex, ok := o.Err.(*exec.ExitError); ok {
		if st, ok := ex.Sys().(syscall.WaitStatus); ok {
			return st.ExitStatus()
		}
	}
	return UnknownExit
}

func newCommand(rdr *bufio.Reader, tmpName string, cmd string, err error) *Command {
	o := &Command{rdr, tmpName, err, cmd}
	if tmpName != "" {
		runtime.SetFinalizer(os.Remove, tmpName)
	}
	return o
}

// Run takes a command string, executes the command, and sends the (realized) output to stdout
// and returns any error
func Run(command string, stdout chan<- *Command, stderr ...io.Writer) error {

	cmd := exec.Command(getShell(), "-c", command)

	opipe, err := cmd.StdoutPipe()
	if err != nil {
		opipe.Close()
		return err
	}

	if len(stderr) != 0 {
		cmd.Stderr = stderr[0]
	} else {
		cmd.Stderr = os.Stderr
	}

	func() {
		err = cmd.Start()
		if err != nil {
			stdout <- newCommand(nil, "", command, err)
			return
		}

		bpipe := bufio.NewReaderSize(opipe, BufferSize)
		var res []byte
		res, err = bpipe.Peek(BufferSize)

		// less than BufferSize bytes in output...
		if err == bufio.ErrBufferFull || err == io.EOF {
			err = cmd.Wait()
			stdout <- newCommand(bufio.NewReader(bytes.NewReader(res)), "", command, err)
			return
		}

		// more than BufferSize bytes in output. must use tmpfile
		var tmp *os.File
		tmp, err = ioutil.TempFile("", "gargsTmp.")
		if err != nil {
			stdout <- newCommand(bufio.NewReader(bytes.NewReader(res)), "", command, err)
			return
		}
		btmp := bufio.NewWriter(tmp)
		_, err = io.CopyBuffer(btmp, bpipe, res)
		if err != nil {
			stdout <- newCommand(bufio.NewReader(bytes.NewReader(res)), "", command, err)
			return
		}
		err = opipe.Close()
		if err != nil {
			return
		}
		btmp.Flush()

		_, err = tmp.Seek(0, 0)
		if err != nil {
			return
		}
		stdout <- newCommand(bufio.NewReader(tmp), tmp.Name(), command, nil)
	}()
	return err
}

// Runner accepts commands from a channel and writes the output, errors to errors.
func Runner(commands <-chan string, stderr ...io.Writer) (stdout chan *Command) {
	stdout = make(chan *Command)

	var oerr io.Writer = os.Stderr
	if len(stderr) > 0 {
		oerr = stderr[0]
	}
	wg := &sync.WaitGroup{}
	wg.Add(runtime.GOMAXPROCS(0))

	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		go func() {
			defer wg.Done()
			for cmd := range commands {
				err := Run(cmd, stdout, stderr...)
				if err != nil {
					oerr.Write([]byte(err.Error()))

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
