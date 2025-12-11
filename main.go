package main

import (
	"fmt"
	"log"

	"github.com/Omorfii/aggregator/internal/config"
)

func main() {

	cfg, err := config.Read()
	if err != nil {
		log.Fatal(err)
	}

	err = cfg.SetUser("Zoe")
	if err != nil {
		log.Fatal(err)
	}

	updatedCfg, err := config.Read()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(updatedCfg)
}
