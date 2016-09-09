<!--
rm -rf binaries
mkdir -p binaries/
VERSION=0.3.3
for os in darwin linux windows; do
	GOOS=$os GOARCH=$arch go build -o binaries/gargs_${os} main.go
done
-->
gargs
=====

[![Build Status](https://travis-ci.org/brentp/gargs.svg?branch=master)](https://travis-ci.org/brentp/gargs)

Work In Progress:

**gargs** is like **xargs** but it addresses the following limitations in xargs:

+ it keeps the output serialized even when using multiple threads
+ easy to specify multiple arguments with number blocks ({0}, {1}, ...) and {} indicates the entire line.
+ easy to use multiple lines to fill command-template.
+ easy to --retry each command if it fails (e.g. due to network or other intermittent error)
+ allows exiting all commands when an error in one of them occurs.
+ optionally logs all commands with successful commands prefixed by '#' so it's easy to find failed commands.
+ simple implementation.
+ expects a $SHELL command as the argument rather than requiring `bash -c ...`


An very simple example usage with 4 processes to echo some numbers:

```
$ seq 5 | gargs --log my.log -p 4 "echo {0}"
1
2
3
4
5
```

my.log will contain the commands run and a final line '# SUCCESS' that shows all processes finished
without error. This makes it easy to assure that all commands ran without error even if the user
didn't catch the exit code of the command.

Install
=======

Download the appropriate binary for your system from [releases](https://github.com/brentp/gargs/releases) into your $PATH.


Implementation
==============

`gargs` will span a worker goroutine for each core requested via `-p`. It will attempt
to read up to 1MB of output from each process into memory. If it reaches an EOF (they
end of the output from the process) within that 1MB, then it will write that to stdout.
If not, it will write to a temporary file TODO keep memory usage low. The output from
each process can then be sent to STDOUT with the only work being the actual copy of
bytes from the temp-file to STDOUT--no waiting on the process itself.

Each process is run via golang's [os/exec#Cmd](https://golang.org/pkg/os/exec/#Cmd) with
output sent to a pipe. There is very little overhead for this per-call; comparing `xargs` to `gargs`:

```
seq 1 5000 | xargs -I {} bash -c 'echo {}' > /dev/null
seq 1 5000 | gargs 'echo {}' > /dev/null
```

gargs takes about 4.6 seconds while xargs takes 4.0 seconds.


Example
=======
Let's say we have a file `t.txt` like:
```
chr1	22 33
chr2 22 33
chr3 22	33
chr4	22	33
```
That has a mixture of tabs and spaces. We can convert each line to chrom:start-end format with:

```
$ cat t.txt | gargs --sep "\s+" -p 2 "echo '{0}:{1}-{2}'"
chr2:22-33
chr1:22-33
chr3:22-33
chr4:22-33
```

In this case, we're using **2** processes to run this in parallel which will make more of a difference
if we do something time-consuming rather than `echo`.

Note that `{0}`, `{1}`, etc. grab the 1st, 2nd, ... values respectively. To get the entire line, use `{}`.

We can use `-n` to send multiple lines of input to each process:

```
$ seq 1 10 | gargs -n 4 "echo {}"
1 2 3 4
5 6 7 8
9 10
```

Note that even though we send 4 arguments, we only specify the place-holder `{}` once.
Also it does the right thing (tm) for the last line where there are only 2 values (9, 10).
This works as long as the program accepting the arguments doesn't required a fixed number.


Usage
=====

via `gargs -h`

```
usage: gargs [--procs PROCS] [--nlines NLINES] [--retry RETRY] [--sep SEP] [--verbose] [--stop-on-error] [--dry-run] [--log LOG] COMMAND

positional arguments:
  command                command to execute.

options:
  --procs PROCS, -p PROCS
                         number of processes to use. [default: 1]
  --nlines NLINES, -n NLINES
                         number of lines to consume for each command. -s and -n are mutually exclusive. [default: 1]
  --retry RETRY, -r RETRY
                         number of times to retry a command if it fails (default is 0).
  --sep SEP, -s SEP      regular expression split line with to fill multiple template spots default is not to split. -s and -n are mutually exclusive.
  --verbose, -v          print commands to stderr as they are executed.
  --stop-on-error, -s    stop execution on any error. default is to report errors and continue execution.
  --dry-run, -d          print (but do not run) the commands.
  --log LOG, -l LOG      file to log commands. Successful commands are prefixed with '#'.
  --help, -h             display this help and exit
```


API
===

There is also a simple API for running shell processes in the process subdirectory with documentation [here](https://godoc.org/github.com/brentp/gargs/process)

[![GoDoc] (https://godoc.org/github.com/brentp/gargs/process?status.png)](https://godoc.org/github.com/brentp/gargs/process)



TODO
====

+ [X] final exit code is the largest of any seen exit code even with -c
+ [X] dry-run
+ [ ] combinations of `-n` and `--sep`.
+ [ ] for example, we are sending regions to bcftools view or tabix. It's faster to send multiple
      queries to each rather than starting a new process for each one.
      if we do 'bcftools view {} {} {}' then we'll get an error at the last round if the input is
      not divisible by 3. For this case, we should be able to just issue a warning.

	  Actually, can currently do: `seq 10 | ./gargs -n 3 "echo {0} {1} {2}"`

