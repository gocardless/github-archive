github-archive
==============

An easy way to archive an entire organisation repos on S3

## Usage

```
$ export GITHUB_TOKEN=...
$ export AWS_ACCESS_KEY_ID=...
$ export AWS_SECRET_ACCESS_KEY=...
$ go build
$ ./github-archive --org github --bucket my-bucket
```

