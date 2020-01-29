package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bombsimon/distcron"
	"github.com/robfig/cron/v3"
)

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
