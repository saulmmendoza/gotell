package cmd

import (
	"fmt"
	"net/http"
	"strings"

	"code.gitea.io/sdk/gitea"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/sirupsen/logrus"
	"github.com/netlify/gotell/api"
	"github.com/netlify/gotell/conf"
	"github.com/netlify/gotell/models"
	"github.com/spf13/cobra"
)

func apiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "api",
		Short: "api",
		Run: func(cmd *cobra.Command, args []string) {
			execWithConfig(cmd, serveAPI)
		},
	}
}

func serveAPI(config *conf.Configuration) {
	if err := verifyAPISettings(config); err != nil {
		logrus.Fatalf("Error verifying settings: %v", err)
	}

	if err := verifySite(config.API.SiteURL); err != nil {
		logrus.Fatalf("Error verifying site: %v", err)
	}

	db, err := gorm.Open(config.DB.Driver, config.DB.URL)
	if err != nil {
		logrus.Fatalf("Error opening database: %v", err)
	}
	defer db.Close()

	db.AutoMigrate(&models.Instance{})

	giteaClient := newGiteaClient(config)
	if err := verifyRepoAndToken(config.API.Repository, giteaClient); err != nil {
		logrus.Fatalf("Error verifying repo: %v", err)
	}

	server := api.NewServerWithVersion(config, giteaClient, db, Version)
	server.ListenAndServe()
}

func verifyAPISettings(config *conf.Configuration) error {
	if config.API.SiteURL == "" {
		return fmt.Errorf("API requires a site url")
	}

	if config.API.Repository == "" {
		return fmt.Errorf("API requires a GitHub repository path")
	}

	if config.API.AccessToken == "" {
		return fmt.Errorf("API requires a GitHub access token")
	}

	return nil
}

func verifySite(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("Expected 200 status code for %v, got %v", url, resp.StatusCode)
	}
	return nil
}

func verifyRepoAndToken(repository string, client *gitea.Client) error {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return fmt.Errorf("Repo format must be owner/repo - %v", repository)
	}

	_, _, err := client.GetRepo(parts[0], parts[1])
	return err
}

func newGiteaClient(config *conf.Configuration) *gitea.Client {
	forgejoURL := config.API.ForgejoURL
	if forgejoURL == "" {
		forgejoURL = "https://v15.next.forgejo.org"
	}
	client, err := gitea.NewClient(forgejoURL, gitea.SetToken(config.API.AccessToken))
	if err != nil {
		logrus.Fatalf("Error creating gitea client: %v", err)
	}
	return client
}
