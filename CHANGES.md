0.3.4
=====

+ Flush Stdout every 2 seconds.
+ Nice String() output for \**Command* that show time to run, error, etc.
+ Colorized errors
+ Fix error/exit-code tracking when a tmpfile is used.
+ remove --continue-on-error (-c) and make that the default. Introduce --stop-on-error (-s).
+ add --log argument where each command is logged. If successful it is prefixed with '#' if not, it is printed as-is. If the entire execution ends succesfully, the last line will be '# SUCCESS' otherwise it will show, e.g. '# FAILED 3 commands'. The failed commands are easily grep'ed from the log with "grep -v ^# $log"
