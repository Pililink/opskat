package command_review_svc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactSecretsReplacesSecretValues(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"mysql attached -p", "mysql -uroot -pS3cret! -e 'show databases'", "mysql -uroot -p*** -e 'show databases'"},
		{"mysqldump attached -p", "mysqldump -h db -u app -pabc123 shop > /tmp/x.sql", "mysqldump -h db -u app -p*** shop > /tmp/x.sql"},
		{"mysql inside docker exec", `docker exec db mysql -uroot -pRootPw -e "DROP DATABASE x"`, `docker exec db mysql -uroot -p*** -e "DROP DATABASE x"`},
		{"--password=", "psql --password=hunter2 -h db", "psql --password=*** -h db"},
		{"--password value", "mongosh --password hunter2 --host db", "mongosh --password *** --host db"},
		{"env assignment", "MYSQL_PWD=abc MYSQL_PASSWORD=xyz mysql -e 'select 1'", "MYSQL_PWD=*** MYSQL_PASSWORD=*** mysql -e 'select 1'"},
		{"export token", "export GITHUB_TOKEN=ghp_x1 && gh repo list", "export GITHUB_TOKEN=*** && gh repo list"},
		{"quoted env value", `API_KEY="a b c" ./run.sh`, `API_KEY=*** ./run.sh`},
		{"json field", `curl -d '{"user":"a","password":"p@ss"}' http://x`, `curl -d '{"user":"a","password":"***"}' http://x`},
		{"identified by", "CREATE USER 'repl'@'%' IDENTIFIED BY 'Repl#2026'", "CREATE USER 'repl'@'%' IDENTIFIED BY '***'"},
		{"identified with by", "ALTER USER u IDENTIFIED WITH mysql_native_password BY 'pw'", "ALTER USER u IDENTIFIED WITH mysql_native_password BY '***'"},
		{"postgres with password", "ALTER USER app WITH PASSWORD 'pg-secret'", "ALTER USER app WITH PASSWORD '***'"},
		{"bearer header", `curl -H "Authorization: Bearer eyJhbGciOi.x.y" https://api`, `curl -H "Authorization: Bearer ***" https://api`},
		{"api key header", `curl -H 'X-Api-Key: k-123' https://api`, `curl -H 'X-Api-Key: ***' https://api`},
		{"url userinfo", "git clone https://bob:tok3n@git.example.com/r.git", "git clone https://bob:***@git.example.com/r.git"},
		{"redis url", "redis-cli -u redis://default:pw@10.0.0.1:6379 ping", "redis-cli -u redis://default:***@10.0.0.1:6379 ping"},
		{"curl -u", "curl -u admin:adminpw http://x", "curl -u admin:*** http://x"},
		{"sshpass", "sshpass -p 'pw' ssh root@h", "sshpass -p *** ssh root@h"},
		{"redis-cli -a", "redis-cli -h r -a s3cr3t info", "redis-cli -h r -a *** info"},
		{"redis AUTH", "AUTH default s3cr3t", "AUTH ***"},
		{"aws access key", "aws configure set aws_access_key_id AKIAIOSFODNN7EXAMPLE", "aws configure set aws_access_key_id AKIA***"},
		{"github token", "echo ghp_abcdefghijklmnopqrstuvwxyz0123456789 | gh auth login --with-token", "echo gh*** | gh auth login --with-token"},
		{"sk key", "OPENAI=1 ./x --key sk-proj-abcdefghijklmnopqrstu", "OPENAI=1 ./x --key sk-***"},
		{"private key", "echo '-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaA\n-----END OPENSSH PRIVATE KEY-----' > k", "echo '-----BEGIN PRIVATE KEY-----***-----END PRIVATE KEY-----' > k"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, RedactSecrets(c.in))
		})
	}
}

func TestRedactSecretsLeavesOrdinaryCommandsAlone(t *testing.T) {
	for _, in := range []string{
		"mkdir -p /data/app",
		"ssh -p 22 root@host uptime",
		"find / -name '*.log' -print",
		"kubectl get pods -n prod -o wide",
		"SELECT id, name FROM users WHERE id = 1",
		"GET session:123",
		"PATH=/usr/bin ls -la",
		"curl https://example.com/health",
		"mysql -uroot -p -e 'select 1'",
	} {
		assert.Equal(t, in, RedactSecrets(in), in)
	}
}
