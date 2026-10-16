package main

import (
	"github.com/MuhametSmaili/hetki/cmd"

	_ "github.com/MuhametSmaili/hetki/internal/backend/tmux"
)

func main() {
	cmd.Execute()
}
