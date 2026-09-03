// Command knott-engine runs the Execution Engine as a standalone service.
package main

import (
	"log"

	"github.com/regnant/knott/internal/execution"
)

func main() {
	if err := execution.Run(); err != nil {
		log.Fatal(err)
	}
}
