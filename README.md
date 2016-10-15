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

**gargs** is like **xargs** but it addresses the following limitations in xargs:

+ it keeps the output serialized (in `xargs` the output one process may be interrupted mid-line by the output from another process) even when using multiple threads
+ easy to specify multiple arguments with number blocks ({0}, {1}, ...) and {} indicates the entire line.
+ easy to use multiple lines to fill command-template.
+ easy to --retry each command if it fails (e.g. due to network or other intermittent error)
+ allows exiting all commands when an error in one of them occurs.
+ optionally logs all commands with successful commands prefixed by '#' so it's easy to find failed commands.
+ simple implementation.
+ expects a $SHELL command as the argument rather than requiring `bash -c ...`
+ allows keeping output in order of input even when proceses finish out of order (via -o flag)


An very simple example usage with 3 processes to echo some numbers:

```
$ seq 3 | gargs --log my.log -p 3 "echo {0}"
1
2
3
```

my.log will contain the commands run and a final line '# SUCCESS' that shows all processes finished
without error. This makes it easy to check if all commands ran without catching the exit code of the command.

Install
=======

Download the appropriate binary for your system from [releases](https://github.com/brentp/gargs/releases) into your $PATH.

Environment Variables
=====================

`GARGS_PROCESS_BUFFER`
----------------------

`GARGS_PROCESS_BUFFER` can be used to set the size of data that can be read into memory before a tmp file is used.
Increasing this value increases memory use and decreases disk IO. E.g. to tell `gargs` to use a tmp file only
when it has read 20MB (for example if we know that most processes will generate less than that). user

```
GARGS_PROCESS_BUFFER=20000000 gargs ...
```

Changing this value will not affect the output at all, it will only change the internal decisions in `gargs`


`GARGS_WAIT_MULTIPLIER`
-----------------------

Increasing this value improves concurrency at the expense of memory when `-o` or `--ordered` is used.
It determines the size of the queue that finished processes will be pushed onto and therefore how many
processes can be waiting will a single (or few) slow process are still running. If the user specified 
`-p` 10 and GARGS_WAIT_MULTIPLIER=5, then up to 49 processes can wait for a singe slow process.
The default value is 4.

Implementation
==============

`gargs` will span a worker goroutine for each core requested via `-p`. It will attempt
to read up to 1MB (settable by `GARGS_PROCESS_BUFFER` env variable) of output from each proceses
into memory. If it reaches an EOF (they end of the output from the process) within that 1MB,
then it will write that to stdout. If not, it will write to a temporary file keep memory usage:
low. The output from each process can then be sent to STDOUT with the only work being the actual copy of
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
gargs 0.3.6
usage: gargs [--procs PROCS] [--sep SEP] [--nlines NLINES] [--retry RETRY] [--ordered] [--verbose] [--stop-on-error] [--dry-run] [--log LOG] COMMAND

positional arguments:
  command                command template to fill and execute.

options:
  --procs PROCS, -p PROCS
                         number of processes to use. [default: 1]
  --sep SEP, -s SEP      regex to split line to fill multiple template place-holders.
  --nlines NLINES, -n NLINES
                         lines to consume for each command. -s and -n are mutually exclusive. [default: 1]
  --retry RETRY, -r RETRY
                         times to retry a command if it fails (default is 0).
  --ordered, -o          keep output in order of input.
  --verbose, -v          print commands to stderr as they are executed.
  --stop-on-error, -s    stop all processes on any error.
  --dry-run, -d          print (but do not run) the commands.
  --log LOG, -l LOG      file to log commands. Successful commands are prefixed with '#'.
  --help, -h             display this help and exit
  --version              display version and exit

```

Environment Variables
=====================

The environment variable `PROCESS_I` is set to the (0-based) line (or batch of lines) number
of the input line it is processing.

For example, this can be used create unique file names.:

```
... | gargs -p 20 'do-stuff $input > $PROCESS_I.output.txt'
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


Extras
======

Transactional
-------------

While this isn't done in `gargs` per se. The user can implement their own transactional setup with something like:

```
... | gargs -p 20 "if [[ ! -e $PROCESS_i.final ]]; then do-stuff {} > $PROCESS_I.tmp && mv $PROCESS_I.tmp $PROCESS_I.final; fi" 
```
Since `mv` is atomic on most systems. This will only ever `do-stuff` sucessfully once. 

