0.3.6 (dev)
===========
+ output gargs version in help.
+ restore --ordered (-o) to keep order of output same as input.
  this will cache 3\*proccesses output waiting for the slowest job to finish.
  This means that if the user requested 10 processes (-p 10) then there could
  be up to 30 finished jobs waiting for a slow job to finish. If these are input
  memory, they are guaranteed to take <= 1MB (+ go's overhead). If they are larger
  than 1MB, then their data will be on disk.
  This is implemented carefully such that the performance penalty will be small
  unless there are few extremely long-running process outliers.
+ set $PROCESS\_I environment variable for each line (or batch of lines).
+ read `PROCESS_BUFFER` to let user set size of data before a tempfile is used.

0.3.4
=====

+ Flush Stdout every 2 seconds.
+ Nice String() output for \**Command* that show time to run, error, etc.
+ Colorized errors
+ Fix error/exit-code tracking when a tmpfile is used.
+ remove --continue-on-error (-c) and make that the default. Introduce --stop-on-error (-s).
+ add --log argument where each command is logged. If successful it is prefixed with '#' if not, it is printed as-is. If the entire execution ends succesfully, the last line will be '# SUCCESS' otherwise it will show, e.g. '# FAILED 3 commands'. The failed commands are easily grep'ed from the log with "grep -v ^# $log"
