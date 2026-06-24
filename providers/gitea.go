package providers

import (
	"code.gitea.io/sdk/gitea"
)

type GiteaProvider struct {
	client *gitea.Client
}

func NewGiteaProvider(url, token string) (*GiteaProvider, error) {
	client, err := gitea.NewClient(url, gitea.SetToken(token))
	if err != nil {
		return nil, err
	}
	return &GiteaProvider{client: client}, nil
}

func (p *GiteaProvider) GetRepo(owner, repo string) error {
	_, _, err := p.client.GetRepo(owner, repo)
	return err
}

func (p *GiteaProvider) GetRepoBranch(owner, repo, branch string) (*Branch, error) {
	b, _, err := p.client.GetRepoBranch(owner, repo, branch)
	if err != nil {
		return nil, err
	}
	return &Branch{
		Name: b.Name,
		Commit: struct{ ID string }{
			ID: b.Commit.ID,
		},
	}, nil
}

func (p *GiteaProvider) CreateBranch(owner, repo, branch, oldBranch string) error {
	_, _, err := p.client.CreateBranch(owner, repo, gitea.CreateBranchOption{
		BranchName:    branch,
		OldBranchName: oldBranch,
	})
	return err
}

func (p *GiteaProvider) CreateFile(owner, repo, filepath string, opt CreateFileOptions) error {
	_, _, err := p.client.CreateFile(owner, repo, filepath, gitea.CreateFileOptions{
		FileOptions: gitea.FileOptions{
			Message:       opt.Message,
			BranchName:    opt.BranchName,
			NewBranchName: opt.NewBranchName,
		},
		Content: opt.ContentBase64,
	})
	return err
}

func (p *GiteaProvider) CreatePullRequest(owner, repo string, opt CreatePullRequestOption) error {
	_, _, err := p.client.CreatePullRequest(owner, repo, gitea.CreatePullRequestOption{
		Title: opt.Title,
		Head:  opt.Head,
		Base:  opt.Base,
		Body:  opt.Body,
	})
	return err
}
