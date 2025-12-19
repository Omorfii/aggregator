package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Omorfii/aggregator/internal/config"
	"github.com/Omorfii/aggregator/internal/database"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type state struct {
	db  *database.Queries
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

	_, err := s.db.GetUser(context.Background(), firstArgument)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("user does not exist")
		}
		return err
	}

	err = s.cfg.SetUser(firstArgument)
	if err != nil {
		return err
	}

	fmt.Println("User has been set")

	return nil
}

func handlerRegister(s *state, cmd command) error {

	if len(cmd.arguments) <= 0 {
		return fmt.Errorf("no username given")
	}

	firstArgument := cmd.arguments[0]

	_, err := s.db.GetUser(context.Background(), firstArgument)
	if err == nil {
		return fmt.Errorf("user already exist")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	uuid := uuid.New()

	parameters := database.CreateUserParams{
		ID:        uuid,
		Name:      firstArgument,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	user, err := s.db.CreateUser(context.Background(), parameters)
	if err != nil {
		return err
	}

	err = s.cfg.SetUser(firstArgument)
	if err != nil {
		return err
	}

	fmt.Printf("User was created: %+v\n", user)

	return nil
}

func handlerReset(s *state, cmd command) error {

	err := s.db.DeleteAllUsers(context.Background())
	if err != nil {
		return err
	}

	fmt.Printf("All users have been deleted\n")
	return nil
}

func handlerUsers(s *state, cmd command) error {

	users, err := s.db.GetUsers(context.Background())
	if err != nil {
		return err
	}

	for i := 0; i < len(users); i++ {
		if users[i].Name == s.cfg.CurrentUser {
			fmt.Printf("%v (current)\n", users[i].Name)
		} else {
			fmt.Printf("%v\n", users[i].Name)
		}
	}

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

	db, err := sql.Open("postgres", cfg.Url)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	dbQueries := database.New(db)

	currentConfig := state{
		db:  dbQueries,
		cfg: &cfg,
	}

	currentCommands := commands{
		handlers: make(map[string]func(*state, command) error),
	}

	currentCommands.register("login", handlerLogin)
	currentCommands.register("register", handlerRegister)
	currentCommands.register("reset", handlerReset)
	currentCommands.register("users", handlerUsers)

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
