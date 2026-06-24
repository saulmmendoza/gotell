package providers

import (
	"codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"
)

type ForgejoProvider struct {
	client *forgejo.Client
}

func NewForgejoProvider(url, token string) (*ForgejoProvider, error) {
	client, err := forgejo.NewClient(url, forgejo.SetToken(token))
	if err != nil {
		return nil, err
	}
	return &ForgejoProvider{client: client}, nil
}

func (p *ForgejoProvider) GetRepo(owner, repo string) error {
	_, _, err := p.client.GetRepo(owner, repo)
	return err
}

func (p *ForgejoProvider) GetRepoBranch(owner, repo, branch string) (*Branch, error) {
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

func (p *ForgejoProvider) CreateBranch(owner, repo, branch, oldBranch string) error {
	_, _, err := p.client.CreateBranch(owner, repo, forgejo.CreateBranchOption{
		BranchName:    branch,
		OldBranchName: oldBranch,
	})
	return err
}

func (p *ForgejoProvider) CreateFile(owner, repo, filepath string, opt CreateFileOptions) error {
	_, _, err := p.client.CreateFile(owner, repo, filepath, forgejo.CreateFileOptions{
		FileOptions: forgejo.FileOptions{
			Message:       opt.Message,
			BranchName:    opt.BranchName,
			NewBranchName: opt.NewBranchName,
		},
		Content: opt.ContentBase64,
	})
	return err
}

func (p *ForgejoProvider) CreatePullRequest(owner, repo string, opt CreatePullRequestOption) error {
	_, _, err := p.client.CreatePullRequest(owner, repo, forgejo.CreatePullRequestOption{
		Title: opt.Title,
		Head:  opt.Head,
		Base:  opt.Base,
		Body:  opt.Body,
	})
	return err
}
