gargs
=====

Work In Progress:

gargs is like xargs but it addresses the following limitations in xargs:

+ it keeps the output serialized even when using multiple threads
+ easy to specify multiple arguments

As an example, this will keep the output in order and send 3 arguments to each process.
It is using 4 proceses to parallelize.

```
$ seq 12 -1 1 | go run main.go -p 4 -n 3 "sleep {}; echo {} {}"
11 10
8 7
5 4
2 1
```

Note that for each line, we slept 12, 9, 6, 3 seconds respectively but the output order was maintained.


For now, the -n 3 is redundant with seeing the "{}"'s. In the future, it may be possible to use numbered arguments:

```
# not currently possible
sleep {0}; echo {1} {2}
```


