package main

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
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

type RSSFeed struct {
	Channel struct {
		Title       string    `xml:"title"`
		Link        string    `xml:"link"`
		Description string    `xml:"description"`
		Item        []RSSItem `xml:"item"`
	} `xml:"channel"`
}

type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func fetchFeed(ctx context.Context, feedURL string) (*RSSFeed, error) {

	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "gator")

	client := &http.Client{}

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	byt, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var feed RSSFeed

	if err = xml.Unmarshal(byt, &feed); err != nil {
		return nil, err
	}

	feed.Channel.Title = html.UnescapeString(feed.Channel.Title)
	feed.Channel.Description = html.UnescapeString(feed.Channel.Description)

	for i, item := range feed.Channel.Item {
		feed.Channel.Item[i].Title = html.UnescapeString(item.Title)
		feed.Channel.Item[i].Description = html.UnescapeString(item.Description)
	}

	return &feed, nil
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

func handlerAgg(_ *state, _ command) error {

	feed, err := fetchFeed(context.Background(), "https://www.wagslane.dev/index.xml")
	if err != nil {
		return err
	}

	fmt.Printf("%+v\n", feed)
	return nil
}

func handlerAddFeed(s *state, cmd command) error {

	if len(cmd.arguments) <= 0 {
		return fmt.Errorf("no feed name and url given")
	}

	if len(cmd.arguments) == 1 {
		return fmt.Errorf("no url given")
	}

	firstArgument := cmd.arguments[0]
	secondArgument := cmd.arguments[1]

	curentUserName := s.cfg.CurrentUser

	curentUser, err := s.db.GetUser(context.Background(), curentUserName)
	if err != nil {
		return err
	}

	uuid := uuid.New()

	parameters := database.CreateFeedParams{
		ID:        uuid,
		Name:      firstArgument,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Url:       secondArgument,
		UserID:    curentUser.ID,
	}

	feed, err := s.db.CreateFeed(context.Background(), parameters)
	if err != nil {
		return err
	}

	fmt.Printf("Feed was created: %+v\n", feed)

	return nil
}

func handlerFeeds(s *state, _ command) error {

	feeds, err := s.db.GetFeeds(context.Background())
	if err != nil {
		return err
	}

	for _, feed := range feeds {

		creator, err := s.db.GetUserFromID(context.Background(), feed.UserID)
		if err != nil {
			return err
		}

		fmt.Printf("Feed name: %v\n", feed.Name)
		fmt.Printf("Feed url: %v\n", feed.Url)
		fmt.Printf("User that created the feed: %v\n", creator)
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
	currentCommands.register("agg", handlerAgg)
	currentCommands.register("addfeed", handlerAddFeed)
	currentCommands.register("feeds", handlerFeeds)

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
