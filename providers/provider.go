package providers

type FileOptions struct {
	Message       string
	BranchName    string
	NewBranchName string
}

type CreateFileOptions struct {
	FileOptions
	ContentBase64 string
}

type CreatePullRequestOption struct {
	Title string
	Head  string
	Base  string
	Body  string
}

type Branch struct {
	Name   string
	Commit struct {
		ID string
	}
}

type GitProvider interface {
	GetRepo(owner, repo string) error
	GetRepoBranch(owner, repo, branch string) (*Branch, error)
	CreateBranch(owner, repo, branch, oldBranch string) error
	CreateFile(owner, repo, filepath string, opt CreateFileOptions) error
	CreatePullRequest(owner, repo string, opt CreatePullRequestOption) error
}
