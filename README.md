# Distributed cron

You know those old crontabs? Yeah, the ones running the old legacy code you're
about to replace. So you just re-implemented it all in Go but... How do you
schedule it? And since you're so modern the new service runs in an all fancy
micro service architecture. And it scales! But that old legacy feature, it
should only be ran once, just like the crontab defined.

## Schedule with locks!

This package uses cron combined with [Redis](https://redis.io/) to schedule the
tasks, use Redis for locks and ensure the service won't shut down while running
a process!

## Usage

```go
func main() {
    dc := distcron.New().
        WithLogger(cron.VerbosePrintfLogger(log.New(os.Stdout, "cron: ", log.LstdFlags))).
        AddJob("* * * * * ", "my-job", myJob)

    if err := dc.Run(); err != nil {
        panic(err)
    }
}

func myJob() {
    fmt.Println("I'm running scheduled!")
    time.Sleep(10 * time.Second)
    fmt.Println("I took 10 seconds!")
}
```

That's it! It will run your job at the given time and ensure that only one
instance of the application can take and execute the job! The lock will be
released when the passed function is done.

## Goals

Provide a simple and easy way to "just make it work". A simple library where
your main interest is to schedule a small amount of simple tasks by passing your
own function. The small worker should be as stable as `robfig/cron` but not
necessarily more stable than so.

This is hopefully used as a temporary (famous last words)
implementation while moving away from this kind of scheduled tasks.

## Non-goals

To create an in-depth and advanced scheduling system for advanced tasks. This is
not a long term replacement for cron or other ways to schedule your jobs. An
advanced interface with support to configure and tweak details of the way your
job is scheduled.

## Logging

See [cron documentation](https://godoc.org/github.com/robfig/cron#Logger) for
information about logging. The logging interface used is based on
[go-logr/logr](https://github.com/go-logr/logr) which have implementations for
several loggers such as `log` from standard library and
[`zap`](https://github.com/uber-go/zap) via
[`zapr`](https://github.com/go-logr/zapr).

## Caveats

* If the job isn't finished until the next time it's being executed it won't run
  again.
* Remember that some deployments have a time limit for graceful stop. AWS ECR
  for example only wait's 15 minutes before killing a container. This means that
  if you roll a container running this job and it takes longer than 15 minutes
  the lock will never be released!

## References

* [robfig/cron](github.com/robfig/cron)
* [go-redsync/redsync]( https://github.com/go-redsync/redsync)
* [gomodule/redigo](https://github.com/gomodule/redigo)
