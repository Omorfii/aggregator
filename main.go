package main

import (
	"fmt"
	"log"
	"os"

	"github.com/Omorfii/aggregator/internal/config"
)

type state struct {
	cfg *config.Config
}

type command struct {
	name      string
	arguments []string
}

func handlerLogin(s *state, cmd command) error {

	if len(cmd.arguments) <= 0 {
		return fmt.Errorf("no username given")
	}

	firstArgument := cmd.arguments[0]

	err := s.cfg.SetUser(firstArgument)
	if err != nil {
		return err
	}

	fmt.Println("User has been set")

	return nil
}

type commands struct {
	handlers map[string]func(*state, command) error
}

func (c *commands) run(s *state, cmd command) error {

	function, exists := c.handlers[cmd.name]
	if exists {
		return function(s, cmd)
	} else {
		return fmt.Errorf("command not found")
	}

}

func (c *commands) register(name string, f func(*state, command) error) error {

	if _, exists := c.handlers[name]; exists {
		return fmt.Errorf("handler for command %s already exists", name)
	}
	c.handlers[name] = f
	return nil
}

func main() {

	cfg, err := config.Read()
	if err != nil {
		log.Fatal(err)
	}

	currentConfig := state{
		cfg: &cfg,
	}

	currentCommands := commands{
		handlers: make(map[string]func(*state, command) error),
	}

	currentCommands.register("login", handlerLogin)

	arguments := os.Args

	if len(arguments) < 2 {
		fmt.Println("not enough argument")
		os.Exit(1)
	}

	userCommand := command{
		name:      arguments[1],
		arguments: arguments[2:],
	}

	if err := currentCommands.run(&currentConfig, userCommand); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}
