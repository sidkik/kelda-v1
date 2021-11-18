package main

import (
	"math/rand"
	"time"

	"github.com/sidkik/kelda-v1/cmd"
	"github.com/sidkik/kelda-v1/cmd/util"
)

func main() {
	// By default, the random number generator is seeded to 1, so the resulting
	// numbers aren't actually different unless we explicitly seed it.
	rand.Seed(time.Now().UnixNano())

	defer util.HandlePanic()
	cmd.Execute()
}
