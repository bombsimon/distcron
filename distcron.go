package distcron

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/go-redsync/redsync"
	"github.com/gomodule/redigo/redis"
	"github.com/robfig/cron/v3"
)

// Job represents one job with it's spec, name and function.
type Job struct {
	Spec string
	Name string
	Func func()
}

// Schedule represents an instance of a schedule.
type Schedule struct {
	jobs      []Job
	logger    cron.Logger
	redisHost string
	redisPort int
	redisDB   int
}

// New creates a new instance of a Scheduke with default values.
func New() *Schedule {
	return &Schedule{
		jobs:      []Job{},
		redisHost: "localhost",
		redisPort: 6379,
		redisDB:   0,
		logger:    cron.DefaultLogger,
	}
}

// WithLogger will set a cron.Logger which is a subset of logr.Logger and use
// that one for logging messages.
func (s *Schedule) WithLogger(l cron.Logger) *Schedule {
	s.logger = l
	return s
}

// WithRedisHost sets the Redis hostname. This is set to "localhost" by default.
func (s *Schedule) WithRedisHost(host string) *Schedule {
	s.redisHost = host
	return s
}

// WithRedisPort will set the redis port. This is set to 6379 by default.
func (s *Schedule) WithRedisPort(port int) *Schedule {
	s.redisPort = port
	return s
}

// WithRedisDB will set the Redis database to use. This is set to 0 by default.
func (s *Schedule) WithRedisDB(db int) *Schedule {
	s.redisDB = db
	return s
}

// AddJob will add a job to the scheduler which will later be added to cron. For
// details about the cron spec, see
// https://godoc.org/github.com/robfig/cron#hdr-CRON_Expression_Format
// The name for the job should be unique because that's what's used to determine
// that only one process run each job.
func (s *Schedule) AddJob(spec, name string, f func()) *Schedule {
	s.jobs = append(s.jobs, Job{
		Spec: spec,
		Name: name,
		Func: f,
	})

	return s
}

// Run will start the schedule process and add all jobs defined to crontab. If
// the connection to the Redis database cannot be established or if a job cannot
// be added an error will be returned.
// The process will run until a signal intteruption occurs. When one is seen the
// teardown process will begin which includes calling stop on the cron runner.
// The stop function will block until all running tasks are finished which means
// that we cannot determine how long the teardown process will take.
func (s *Schedule) Run() error {
	var (
		running = make(chan struct{})
		c       = cron.New(cron.WithLogger(s.logger))
		uri     = url.URL{
			Scheme: "redis",
			Host:   net.JoinHostPort(s.redisHost, strconv.Itoa(s.redisPort)),
			Path:   strconv.Itoa(s.redisDB),
		}
		redisPool = &redis.Pool{Dial: func() (redis.Conn, error) {
			return redis.DialURL(uri.String())
		},
		}
	)

	// Ensure we're connected to Redis.
	if _, err := redisPool.Get().Do("PING"); err != nil {
		return err
	}

	go func() {
		gracefulStop := make(chan os.Signal, 1)

		signal.Notify(gracefulStop, syscall.SIGTERM)
		signal.Notify(gracefulStop, syscall.SIGINT)

		<-gracefulStop

		// Stop the cron job. This will return a context that will wait until
		// jobs are finished. We'll block at the done channel until it's closed,
		// then we'll exit our application.
		<-c.Stop().Done()

		close(running)
	}()

	for _, job := range s.jobs {
		_, err := c.AddFunc(job.Spec, s.lock(redisPool, job.Name, job.Func))
		if err != nil {
			return err
		}
	}

	s.logger.Info("starting jobs")

	c.Run()

	s.logger.Info("caught shutdown signal, starting teardown")

	// Hang until signal interruption is closing the running channel.
	<-running

	s.logger.Info("teardown process completed")

	return nil
}

// lock will take a lock, write a key for the specific job to avoid other
// processes starting the same and then release the lock. When the process is
// finished, the key holding the lock will be removed.
func (s *Schedule) lock(pool redsync.Pool, name string, f func()) func() {
	var (
		rs        = redsync.New([]redsync.Pool{pool})
		mutexName = fmt.Sprintf("GLOBAL-%s", name)
		mutex     = rs.NewMutex(mutexName)
	)

	return func() {
		// Ensure we've got a global lock for the specific task.
		if err := mutex.Lock(); err != nil {
			s.logger.Error(err, "could not obtain lock")
			return
		}

		// Check if the task is already on-going. This is indicated by writing a
		// row with the task name in the Redis database.
		key, err := pool.Get().Do("GET", name)
		if err != nil {
			s.logger.Error(err, "could not get unique key, not running")
			return
		}

		if key != nil {
			s.logger.Info("wasn't first to take the job, aborting")
			return
		}

		// Ensure we write to the database telling we will run the job befor
		// releasing the lock. This will make other processes see that the job
		// was picked up by someone else.
		if _, err := pool.Get().Do("SET", name, 1); err != nil {
			s.logger.Error(err, "could not set job key, not running")
			return
		}

		if !mutex.Unlock() {
			s.logger.Error(errors.New("unlock failed"), "unlock did not return a true value")
		}

		s.logger.Info("staring job")

		// Invoke the user defined function.
		f()

		s.logger.Info("job finished, removing job lock")

		// Take a lock before removing the status of the job begin ran. This is
		// so that noone will try to start the job in the unlock process.
		if err := mutex.Lock(); err != nil {
			s.logger.Error(err, "lock not obtained")
		}

		// Remove the indication for job task.
		if _, err := pool.Get().Do("DEL", name); err != nil {
			s.logger.Error(err, "could not remove job lock")
		}

		if !mutex.Unlock() {
			s.logger.Error(errors.New("unlock failed"), "unlock did not return a true value")
		}
	}
}
