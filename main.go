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
	"strconv"
	"strings"
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

func handlerAgg(s *state, cmd command) error {

	if len(cmd.arguments) <= 0 {
		return fmt.Errorf("no time given")
	}

	firstArgument := cmd.arguments[0]

	timeDuration, err := time.ParseDuration(firstArgument)
	if err != nil {
		return nil
	}

	fmt.Printf("Collecting feeds every %v\n", timeDuration)

	ticker := time.NewTicker(timeDuration)
	for ; ; <-ticker.C {
		err := scrapeFeeds(s, cmd)
		if err != nil {
			return err
		}
		fmt.Printf("scrapeFeeds success\n")
	}

}

func handlerAddFeed(s *state, cmd command, user database.User) error {

	if len(cmd.arguments) <= 0 {
		return fmt.Errorf("no feed name and url given")
	}

	if len(cmd.arguments) == 1 {
		return fmt.Errorf("no url given")
	}

	firstArgument := cmd.arguments[0]
	secondArgument := cmd.arguments[1]

	newFeedID := uuid.New()

	parameters := database.CreateFeedParams{
		ID:        newFeedID,
		Name:      firstArgument,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Url:       secondArgument,
		UserID:    user.ID,
	}

	feed, err := s.db.CreateFeed(context.Background(), parameters)
	if err != nil {
		return err
	}

	secondParameter := database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		UserID:    user.ID,
		FeedID:    newFeedID,
	}

	_, err = s.db.CreateFeedFollow(context.Background(), secondParameter)
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

func handlerFollow(s *state, cmd command, user database.User) error {

	if len(cmd.arguments) <= 0 {
		return fmt.Errorf("no feed url given")
	}

	firstArgument := cmd.arguments[0]

	feedFromURL, err := s.db.GetFeed(context.Background(), firstArgument)
	if err != nil {
		return err
	}

	uuid := uuid.New()

	parameters := database.CreateFeedFollowParams{
		ID:        uuid,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		UserID:    user.ID,
		FeedID:    feedFromURL.ID,
	}

	feedFollows, err := s.db.CreateFeedFollow(context.Background(), parameters)
	if err != nil {
		return err
	}

	fmt.Printf("User %v just followed feed %v", feedFollows.UserName, feedFollows.FeedName)

	return nil
}

func handlerFollowing(s *state, _ command, user database.User) error {

	feedsFollowed, err := s.db.GetFeedFollowsForUser(context.Background(), user.ID)
	if err != nil {
		return err
	}

	for _, feedFollow := range feedsFollowed {

		feed, err := s.db.GetFeedFromID(context.Background(), feedFollow.FeedID)
		if err != nil {
			return err
		}

		fmt.Printf(feed.Name)
	}

	return nil
}

func handlerUnfollow(s *state, cmd command, user database.User) error {

	if len(cmd.arguments) <= 0 {
		return fmt.Errorf("no feed url given")
	}

	firstArgument := cmd.arguments[0]

	feedFromURL, err := s.db.GetFeed(context.Background(), firstArgument)
	if err != nil {
		return err
	}

	parameter := database.UnfollowFeedParams{
		UserID: user.ID,
		FeedID: feedFromURL.ID,
	}

	return s.db.UnfollowFeed(context.Background(), parameter)
}

func scrapeFeeds(s *state, _ command) error {

	feedFetched, err := s.db.GetNextFeedToFetch(context.Background())
	if err != nil {
		return err
	}

	err = s.db.MarkFeedFetched(context.Background(), feedFetched.ID)
	if err != nil {
		return err
	}

	rssFeed, err := fetchFeed(context.Background(), feedFetched.Url)
	if err != nil {
		return err
	}

	var pubdate sql.NullTime

	for _, item := range rssFeed.Channel.Item {

		description := sql.NullString{
			String: item.Description,
			Valid:  item.Description != "",
		}

		pubtime, err := time.Parse(time.RFC1123Z, item.PubDate)
		if err != nil {
			pubdate = sql.NullTime{
				Time:  time.Time{},
				Valid: false,
			}
		} else {
			pubdate = sql.NullTime{
				Time:  pubtime,
				Valid: true,
			}
		}

		parameter := database.CreatePostParams{
			ID:          uuid.New(),
			Title:       item.Title,
			Url:         item.Link,
			Description: description,
			PublishedAt: pubdate,
			FeedID:      feedFetched.ID,
		}

		post, err := s.db.CreatePost(context.Background(), parameter)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "UNIQUE constraint") {
				continue
			}
			return err
		}

		fmt.Printf("post successfully created: %v", post)

	}

	return nil
}

func handlerBrowse(s *state, cmd command, user database.User) error {

	var arg int32

	if len(cmd.arguments) <= 0 {
		arg = 2
	} else {
		val, err := strconv.Atoi(cmd.arguments[0])
		if err != nil {
			return err
		}
		arg = int32(val)
	}

	parameter := database.GetPostsForUserParams{
		UserID: user.ID,
		Limit:  arg,
	}

	posts, err := s.db.GetPostsForUser(context.Background(), parameter)
	if err != nil {
		return nil
	}

	for _, post := range posts {
		fmt.Printf("%v\n", post)
	}

	return nil
}

func middlewareLoggedIn(handler func(s *state, cmd command, user database.User) error) func(*state, command) error {

	return func(s *state, cmd command) error {

		curentUserName := s.cfg.CurrentUser

		curentUser, err := s.db.GetUser(context.Background(), curentUserName)
		if err != nil {
			return err
		}

		return handler(s, cmd, curentUser)
	}
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
	currentCommands.register("addfeed", middlewareLoggedIn(handlerAddFeed))
	currentCommands.register("feeds", handlerFeeds)
	currentCommands.register("follow", middlewareLoggedIn(handlerFollow))
	currentCommands.register("following", middlewareLoggedIn(handlerFollowing))
	currentCommands.register("unfollow", middlewareLoggedIn(handlerUnfollow))
	currentCommands.register("browse", middlewareLoggedIn(handlerBrowse))

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
