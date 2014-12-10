package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"code.google.com/p/goauth2/oauth"
	"github.com/google/go-github/github"
	"github.com/rlmcpherson/s3gof3r"
)

var (
	org        = flag.String("org", "", "Organisation")
	bucketName = flag.String("bucket", "", "Upload bucket")

	githubClient *github.Client
	bucket       *s3gof3r.Bucket
	date         string
)

type Repo struct {
	Date  string
	Owner string
	Name  string
	URL   string
}

func main() {
	flag.Parse()

	github_token := os.Getenv("GITHUB_TOKEN")
	awsAccessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	awsSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	t := &oauth.Transport{
		Token: &oauth.Token{AccessToken: github_token},
	}
	githubClient = github.NewClient(t.Client())

	keys := s3gof3r.Keys{
		AccessKey: awsAccessKey,
		SecretKey: awsSecretKey,
	}
	s3 := s3gof3r.New("", keys)
	bucket = s3.Bucket(*bucketName)

	repoChan := make(chan Repo)

	wg := new(sync.WaitGroup)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go worker(repoChan, wg)
	}

	err := uploadReposForOrg(repoChan, *org)
	if err != nil {
		log.Fatal(err)
	}

	close(repoChan)
	wg.Wait()
}

func uploadReposForOrg(repoChan chan Repo, org string) error {
	now := time.Now().Format("20060102150405")

	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 30},
	}
	for {
		repos, resp, err := githubClient.Repositories.ListByOrg(org, opt)
		if err != nil {
			return err
		}

		for _, repo := range repos {
			r := Repo{
				Date:  now,
				Owner: *repo.Owner.Login,
				Name:  *repo.Name,
				URL:   *repo.SSHURL,
			}
			repoChan <- r
		}

		if resp.NextPage == 0 {
			break
		}
		opt.ListOptions.Page = resp.NextPage
	}

	return nil
}

func worker(repoChan chan Repo, wg *sync.WaitGroup) {
	defer wg.Done()

	for repo := range repoChan {
		n, err := uploadRepositoryToS3(bucket, repo)
		if err != nil {
			fmt.Printf("Error while downloading %s: %s\n", repo.URL, err)
		}
		if n != 0 {
			fmt.Printf("Successfully uploaded %s (%d bytes)\n", repo.URL, n)
		}
	}
}

func uploadRepositoryToS3(bucket *s3gof3r.Bucket, repo Repo) (int64, error) {
	tmp, err := ioutil.TempDir("", "gh-archive-")
	if err != nil {
		return 0, err
	}
	defer cleanup(tmp)

	cloneDirectory := fmt.Sprintf("%s-%s-%s", repo.Owner, repo.Name, repo.Date)
	err = cloneRepo(tmp, repo.URL, cloneDirectory)
	if err != nil {
		return 0, err
	}

	archive := cloneDirectory + ".tar.gz"
	err = archiveRepo(tmp, archive, cloneDirectory)
	if err != nil {
		return 0, err
	}

	archiveFile, err := os.Open(filepath.Join(tmp, archive))
	if err != nil {
		return 0, err
	}
	defer archiveFile.Close()

	s3Key := fmt.Sprintf("%s/%s/%s-%s.tar.gz", repo.Date, repo.Owner, repo.Name, repo.Date)
	w, err := bucket.PutWriter(s3Key, nil, nil)
	if err != nil {
		return 0, err
	}

	n, err := io.Copy(w, archiveFile)
	if err != nil {
		return 0, err
	}

	if err = w.Close(); err != nil {
		return 0, err
	}

	return n, nil
}

func cloneRepo(cmdDir, repoURL, directory string) error {
	cmd := exec.Command("git", "clone", repoURL, directory)
	cmd.Dir = cmdDir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func archiveRepo(cmdDir, archiveFile, directory string) error {
	cmd := exec.Command("tar", "cvzf", archiveFile, directory)
	cmd.Dir = cmdDir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func cleanup(path string) {
	err := os.RemoveAll(path)
	if err != nil {
		log.Fatal(err)
	}
}
