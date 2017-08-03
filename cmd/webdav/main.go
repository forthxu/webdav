package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/hacdias/webdav"
	wd "golang.org/x/net/webdav"
	"gopkg.in/yaml.v2"
)

var (
	config         string
	defaultConfigs = []string{"config.json", "config.yaml", "config.yml"}
)

func init() {
	flag.StringVar(&config, "config", "config.yaml", "Configuration file")
}

func parseRules(raw []map[string]interface{}) []*webdav.Rule {
	rules := []*webdav.Rule{}

	for _, r := range raw {
		rule := &webdav.Rule{
			Regex: false,
			Allow: false,
			Path:  "",
		}

		if regex, ok := r["regex"].(bool); ok {
			rule.Regex = regex
		}

		if allow, ok := r["allow"].(bool); ok {
			rule.Allow = allow
		}

		path, ok := r["rule"].(string)
		if !ok {
			continue
		}

		if rule.Regex {
			rule.Regexp = regexp.MustCompile(path)
		} else {
			rule.Path = path
		}

		rules = append(rules, rule)
	}

	return rules
}

func parseUsers(raw []map[string]interface{}, c *cfg) {
	for _, r := range raw {
		username, ok := r["username"].(string)
		if !ok {
			panic("user needs an username")
		}

		password, ok := r["password"].(string)
		if !ok {
			panic("user needs a password")
		}

		c.auth[username] = password

		user := &webdav.User{
			Scope:  c.webdav.User.Scope,
			Modify: c.webdav.User.Modify,
			Rules:  c.webdav.User.Rules,
		}

		if scope, ok := r["scope"].(string); ok {
			user.Scope = scope
		}

		if modify, ok := r["modify"].(bool); ok {
			user.Modify = modify
		}

		if rules, ok := r["rules"].([]map[string]interface{}); ok {
			user.Rules = parseRules(rules)
		}

		user.Handler = &wd.Handler{
			FileSystem: wd.Dir(user.Scope),
			LockSystem: wd.NewMemLS(),
		}

		c.webdav.Users[username] = user
	}
}

func getConfig() []byte {
	if config == "" {
		for _, v := range defaultConfigs {
			_, err := os.Stat(v)
			if err == nil {
				config = v
				break
			}
		}
	}

	if config == "" {
		panic(errors.New("no config file"))
	}

	file, err := ioutil.ReadFile(config)
	if err != nil {
		panic(err)
	}

	return file
}

type cfg struct {
	webdav *webdav.Config
	port   string
	auth   map[string]string
}

func parseConfig() *cfg {
	file := getConfig()

	data := struct {
		Port   string                   `json:"port" yaml:"port"`
		Scope  string                   `json:"scope" yaml:"scope"`
		Modify bool                     `json:"modify" yaml:"modify"`
		Rules  []map[string]interface{} `json:"rules" yaml:"rules"`
		Users  []map[string]interface{} `json:"users" yaml:"users"`
	}{
		Port:   "80",
		Scope:  "./",
		Modify: true,
	}

	var err error
	if filepath.Ext(config) == ".json" {
		err = json.Unmarshal(file, &data)
	} else {
		err = yaml.Unmarshal(file, &data)
	}

	if err != nil {
		panic(err)
	}

	config := &cfg{
		port: data.Port,
		auth: map[string]string{},
		webdav: &webdav.Config{
			BaseURL: "",
			User: &webdav.User{
				Scope:  data.Scope,
				Modify: data.Modify,
				Rules:  []*webdav.Rule{},
				Handler: &wd.Handler{
					FileSystem: wd.Dir(data.Scope),
					LockSystem: wd.NewMemLS(),
				},
			},
			Users: map[string]*webdav.User{},
		},
	}

	if len(data.Users) == 0 {
		panic("no user defined")
	}

	if len(data.Rules) != 0 {
		config.webdav.User.Rules = parseRules(data.Rules)
	}

	parseUsers(data.Users, config)
	return config
}

func basicAuth(c *cfg) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)

		username, password, authOK := r.BasicAuth()
		if authOK == false {
			http.Error(w, "Not authorized", 401)
			return
		}

		p, ok := c.auth[username]
		if !ok {
			http.Error(w, "Not authorized", 401)
			return
		}

		if password != p {
			http.Error(w, "Not authorized", 401)
			return
		}

		c.webdav.ServeHTTP(w, r)
	})
}

func main() {
	flag.Parse()
	cfg := parseConfig()
	handler := basicAuth(cfg)

	if err := http.ListenAndServe(":"+cfg.port, handler); err != nil {
		panic(err)
	}
}