// Command knott-agents runs the Agent Integration service as a standalone
// service.
package main

import (
	"log"

	"github.com/regnant/knott/internal/agents"
)

func main() {
	if err := agents.Run(); err != nil {
		log.Fatal(err)
	}
}
