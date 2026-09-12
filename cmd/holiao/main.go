// Command holiao is the Holiaokho CLI. It talks to the same /api/v1 the Web
// UI uses; anything the UI can do, holiao can do from a script.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

type cliConfig struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Token    string `yaml:"token"`
}

var (
	cfg     cliConfig
	cfgPath string
	output  string
	client  = &http.Client{Timeout: 10 * time.Minute}
)

func configFile() string {
	if p := os.Getenv("HOLIAO_CONFIG"); p != "" {
		return p
	}
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "holiao", "config.yaml")
}

func loadConfig() {
	cfgPath = configFile()
	if b, err := os.ReadFile(cfgPath); err == nil {
		yaml.Unmarshal(b, &cfg)
	}
	if u := os.Getenv("HOLIAO_URL"); u != "" {
		cfg.URL = u
	}
	if t := os.Getenv("HOLIAO_TOKEN"); t != "" {
		cfg.Token = t
	}
}

func saveConfig() error {
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		return err
	}
	b, _ := yaml.Marshal(cfg)
	return os.WriteFile(cfgPath, b, 0o600)
}

// ------------------------------------------------------------- HTTP helper

type apiError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return T("error.http", e.Status, e.Message) }

func do(method, path string, body any, out any) error {
	if cfg.URL == "" {
		return errors.New(T("error.no_server"))
	}
	var rdr io.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
	case io.Reader:
		rdr = b
		ct = "application/octet-stream"
	default:
		j, _ := json.Marshal(b)
		rdr = bytes.NewReader(j)
		ct = "application/json"
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(cfg.URL, "/")+path, rdr)
	if err != nil {
		return err
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	req.Header.Set("Accept-Language", os.Getenv("HOLIAO_LANG"))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		ae := &apiError{Status: resp.StatusCode}
		json.NewDecoder(resp.Body).Decode(ae)
		if ae.Message == "" {
			ae.Message = http.StatusText(resp.StatusCode)
		}
		return ae
	}
	if out == nil {
		return nil
	}
	if w, ok := out.(io.Writer); ok {
		_, err = io.Copy(w, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func table(header []string, rows [][]string) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------- commands

func main() {
	initLocale()
	loadConfig()
	root := &cobra.Command{Use: "holiao", Short: "Holiaokho (hó-liāu-khòo) repository manager CLI", SilenceUsage: true}
	root.PersistentFlags().StringVarP(&output, "output", "o", "table", "output format: table|json")

	root.AddCommand(cmdLogin(), cmdStatus(), cmdRepo(), cmdPush(), cmdPull(), cmdSearch(), cmdUser(), cmdToken(), cmdTask(), cmdRole())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func cmdLogin() *cobra.Command {
	var user, pass string
	c := &cobra.Command{Use: "login <url>", Short: "Log in and store a token", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		cfg.URL = strings.TrimSuffix(args[0], "/")
		cfg.Token = ""
		in := bufio.NewReader(os.Stdin)
		if user == "" {
			fmt.Print(T("login.prompt.user"))
			user, _ = in.ReadString('\n')
			user = strings.TrimSpace(user)
		}
		if pass == "" {
			fmt.Print(T("login.prompt.pass"))
			if term.IsTerminal(int(syscall.Stdin)) {
				b, _ := term.ReadPassword(int(syscall.Stdin))
				pass = string(b)
				fmt.Println()
			} else {
				pass, _ = in.ReadString('\n')
				pass = strings.TrimSpace(pass)
			}
		}
		// Basic auth once to mint a long-lived token.
		req, _ := http.NewRequest(http.MethodPost, cfg.URL+"/api/v1/me/tokens", strings.NewReader(`{"name":"holiao cli"}`))
		req.SetBasicAuth(user, pass)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			ae := &apiError{Status: resp.StatusCode}
			json.NewDecoder(resp.Body).Decode(ae)
			return ae
		}
		var out struct {
			Secret string `json:"secret"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		cfg.Username, cfg.Token = user, out.Secret
		if err := saveConfig(); err != nil {
			return err
		}
		fmt.Println(T("login.ok", cfg.URL, user))
		return nil
	}}
	c.Flags().StringVarP(&user, "username", "u", "", "username")
	c.Flags().StringVarP(&pass, "password", "p", "", "password (prefer the prompt or HOLIAO_TOKEN)")
	return c
}

func cmdStatus() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Server status and health checks", RunE: func(cmd *cobra.Command, args []string) error {
		var st map[string]any
		if err := do("GET", "/api/v1/status", nil, &st); err != nil {
			return err
		}
		var chk map[string]any
		do("GET", "/api/v1/status/check", nil, &chk)
		if output == "json" {
			printJSON(map[string]any{"status": st, "check": chk})
			return nil
		}
		fmt.Printf("%s %s  uptime %s  formats %v\n", st["name"], st["version"], st["uptime"], st["formats"])
		if checks, ok := chk["checks"].(map[string]any); ok {
			var rows [][]string
			for k, v := range checks {
				m, _ := v.(map[string]any)
				status := "ok"
				if h, _ := m["healthy"].(bool); !h {
					status = "FAIL"
				}
				msg, _ := m["message"].(string)
				rows = append(rows, []string{k, status, msg})
			}
			table([]string{"CHECK", "STATUS", "MESSAGE"}, rows)
		}
		return nil
	}}
}

func cmdRepo() *cobra.Command {
	c := &cobra.Command{Use: "repo", Short: "Manage repositories"}
	c.AddCommand(&cobra.Command{Use: "ls", Short: "List repositories", RunE: func(cmd *cobra.Command, args []string) error {
		var repos []map[string]any
		if err := do("GET", "/api/v1/repositories", nil, &repos); err != nil {
			return err
		}
		if output == "json" {
			printJSON(repos)
			return nil
		}
		var rows [][]string
		for _, r := range repos {
			st, _ := r["stats"].(map[string]any)
			rows = append(rows, []string{fmt.Sprint(r["name"]), fmt.Sprint(r["format"]), fmt.Sprint(r["type"]), fmt.Sprintf("%v", st["packages"]), human(st["size"]), fmt.Sprint(r["url"])})
		}
		table([]string{"NAME", "FORMAT", "TYPE", "PACKAGES", "SIZE", "URL"}, rows)
		return nil
	}})
	c.AddCommand(&cobra.Command{Use: "get <name>", Short: "Show a repository", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var r map[string]any
		if err := do("GET", "/api/v1/repositories/"+args[0], nil, &r); err != nil {
			return err
		}
		printJSON(r)
		return nil
	}})
	var format, typ, remote, members, write, attrs string
	create := &cobra.Command{Use: "create <name>", Short: "Create a repository", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a := map[string]any{}
		if attrs != "" {
			if err := json.Unmarshal([]byte(attrs), &a); err != nil {
				return fmt.Errorf("--attributes: %w", err)
			}
		}
		if remote != "" {
			a["proxy"] = map[string]any{"remoteUrl": remote}
		}
		if members != "" {
			a["group"] = map[string]any{"members": strings.Split(members, ",")}
		}
		if write != "" {
			a["hosted"] = map[string]any{"writePolicy": write}
		}
		body := map[string]any{"name": args[0], "format": format, "type": typ, "attributes": a}
		var r map[string]any
		if err := do("POST", "/api/v1/repositories", body, &r); err != nil {
			return err
		}
		fmt.Println(T("repo.created", args[0]))
		return nil
	}}
	create.Flags().StringVar(&format, "format", "", "maven|npm|docker|...")
	create.Flags().StringVar(&typ, "type", "", "hosted|proxy|group")
	create.Flags().StringVar(&remote, "remote", "", "proxy remote URL")
	create.Flags().StringVar(&members, "members", "", "group members, comma separated")
	create.Flags().StringVar(&write, "write-policy", "", "hosted write policy: allow|allow_once|deny")
	create.Flags().StringVar(&attrs, "attributes", "", "raw attributes JSON (format-specific blocks)")
	create.MarkFlagRequired("format")
	create.MarkFlagRequired("type")
	c.AddCommand(create)
	c.AddCommand(&cobra.Command{Use: "rm <name>", Short: "Delete a repository", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := do("DELETE", "/api/v1/repositories/"+args[0], nil, nil); err != nil {
			return err
		}
		fmt.Println(T("repo.deleted", args[0]))
		return nil
	}})
	c.AddCommand(&cobra.Command{Use: "browse <name> [path]", Short: "List a directory", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		p := ""
		if len(args) == 2 {
			p = args[1]
		}
		var r struct {
			Directories []string         `json:"directories"`
			Files       []map[string]any `json:"files"`
		}
		if err := do("GET", "/api/v1/repositories/"+args[0]+"/browse?path="+p, nil, &r); err != nil {
			return err
		}
		if output == "json" {
			printJSON(r)
			return nil
		}
		for _, d := range r.Directories {
			fmt.Println(d + "/")
		}
		for _, f := range r.Files {
			fmt.Printf("%s\t%s\n", human(f["size"]), filepath.Base(fmt.Sprint(f["path"])))
		}
		return nil
	}})
	c.AddCommand(&cobra.Command{Use: "invalidate <name>", Short: "Invalidate a proxy repository's cache", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return do("POST", "/api/v1/repositories/"+args[0]+"/invalidate-cache", nil, nil)
	}})
	return c
}

func cmdPush() *cobra.Command {
	var path string
	c := &cobra.Command{Use: "push <repo> <file>", Short: "Upload a file to a hosted repository", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		f, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer f.Close()
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if path == "" {
			path = filepath.Base(args[1])
		}
		mw.WriteField("path", path)
		fw, _ := mw.CreateFormFile("file", filepath.Base(args[1]))
		n, _ := io.Copy(fw, f)
		mw.Close()
		req, _ := http.NewRequest("POST", strings.TrimSuffix(cfg.URL, "/")+"/api/v1/repositories/"+args[0]+"/upload", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			ae := &apiError{Status: resp.StatusCode}
			json.NewDecoder(resp.Body).Decode(ae)
			return ae
		}
		fmt.Println(T("push.ok", path, args[0], n))
		return nil
	}}
	c.Flags().StringVar(&path, "path", "", "destination path inside the repository (default: file name)")
	return c
}

func cmdPull() *cobra.Command {
	var out string
	c := &cobra.Command{Use: "pull <repo> <path>", Short: "Download a file from a repository", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if out == "" {
			out = filepath.Base(args[1])
		}
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer f.Close()
		cw := &countWriter{w: f}
		if err := do("GET", "/repository/"+args[0]+"/"+strings.TrimPrefix(args[1], "/"), nil, cw); err != nil {
			return err
		}
		fmt.Println(T("pull.ok", out, cw.n))
		return nil
	}}
	c.Flags().StringVarP(&out, "output-file", "O", "", "output file name")
	return c
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func cmdSearch() *cobra.Command {
	var format, repo string
	c := &cobra.Command{Use: "search <query>", Short: "Search packages", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var hits []map[string]any
		q := "/api/v1/search?q=" + args[0]
		if format != "" {
			q += "&format=" + format
		}
		if repo != "" {
			q += "&repository=" + repo
		}
		if err := do("GET", q, nil, &hits); err != nil {
			return err
		}
		if output == "json" {
			printJSON(hits)
			return nil
		}
		var rows [][]string
		for _, h := range hits {
			rows = append(rows, []string{fmt.Sprint(h["repository"]), fmt.Sprint(h["namespace"]), fmt.Sprint(h["name"]), fmt.Sprint(h["version"])})
		}
		table([]string{"REPOSITORY", "NAMESPACE", "NAME", "VERSION"}, rows)
		return nil
	}}
	c.Flags().StringVar(&format, "format", "", "filter by format")
	c.Flags().StringVar(&repo, "repo", "", "filter by repository")
	return c
}

func cmdUser() *cobra.Command {
	c := &cobra.Command{Use: "user", Short: "Manage users"}
	c.AddCommand(&cobra.Command{Use: "ls", Short: "List users", RunE: func(cmd *cobra.Command, args []string) error {
		var us []map[string]any
		if err := do("GET", "/api/v1/users", nil, &us); err != nil {
			return err
		}
		if output == "json" {
			printJSON(us)
			return nil
		}
		var rows [][]string
		for _, u := range us {
			rows = append(rows, []string{fmt.Sprint(u["username"]), fmt.Sprint(u["displayName"]), fmt.Sprint(u["email"]), fmt.Sprint(u["roles"]), fmt.Sprint(u["active"])})
		}
		table([]string{"USERNAME", "NAME", "EMAIL", "ROLES", "ACTIVE"}, rows)
		return nil
	}})
	var pass, roles, email string
	create := &cobra.Command{Use: "create <username>", Short: "Create a user", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{"username": args[0], "password": pass, "email": email, "roles": strings.Split(roles, ",")}
		if err := do("POST", "/api/v1/users", body, nil); err != nil {
			return err
		}
		fmt.Println(T("user.created", args[0]))
		return nil
	}}
	create.Flags().StringVar(&pass, "password", "", "password")
	create.Flags().StringVar(&roles, "roles", "developer", "roles, comma separated")
	create.Flags().StringVar(&email, "email", "", "email")
	create.MarkFlagRequired("password")
	c.AddCommand(create)
	c.AddCommand(&cobra.Command{Use: "rm <username>", Short: "Delete a user", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return do("DELETE", "/api/v1/users/"+args[0], nil, nil)
	}})
	return c
}

func cmdRole() *cobra.Command {
	c := &cobra.Command{Use: "role", Short: "Manage roles"}
	c.AddCommand(&cobra.Command{Use: "ls", Short: "List roles", RunE: func(cmd *cobra.Command, args []string) error {
		var rs []map[string]any
		if err := do("GET", "/api/v1/roles", nil, &rs); err != nil {
			return err
		}
		if output == "json" {
			printJSON(rs)
			return nil
		}
		var rows [][]string
		for _, r := range rs {
			pj, _ := json.Marshal(r["privileges"])
			rows = append(rows, []string{fmt.Sprint(r["id"]), fmt.Sprint(r["name"]), string(pj)})
		}
		table([]string{"ID", "NAME", "PRIVILEGES"}, rows)
		return nil
	}})
	return c
}

func cmdToken() *cobra.Command {
	c := &cobra.Command{Use: "token", Short: "Manage your API tokens"}
	c.AddCommand(&cobra.Command{Use: "ls", Short: "List tokens", RunE: func(cmd *cobra.Command, args []string) error {
		var ts []map[string]any
		if err := do("GET", "/api/v1/me/tokens", nil, &ts); err != nil {
			return err
		}
		if output == "json" {
			printJSON(ts)
			return nil
		}
		var rows [][]string
		for _, t := range ts {
			rows = append(rows, []string{fmt.Sprint(t["id"]), fmt.Sprint(t["name"]), fmt.Sprint(t["prefix"]), fmt.Sprint(t["createdAt"]), fmt.Sprint(t["lastUsedAt"])})
		}
		table([]string{"ID", "NAME", "PREFIX", "CREATED", "LAST USED"}, rows)
		return nil
	}})
	var name string
	create := &cobra.Command{Use: "create", Short: "Create a token (for CI)", RunE: func(cmd *cobra.Command, args []string) error {
		var out struct {
			Secret string `json:"secret"`
		}
		if err := do("POST", "/api/v1/me/tokens", map[string]any{"name": name}, &out); err != nil {
			return err
		}
		fmt.Println(T("token.created", out.Secret))
		return nil
	}}
	create.Flags().StringVar(&name, "name", "ci", "token name")
	c.AddCommand(create)
	c.AddCommand(&cobra.Command{Use: "rm <id>", Short: "Revoke a token", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return do("DELETE", "/api/v1/me/tokens/"+args[0], nil, nil)
	}})
	return c
}

func cmdTask() *cobra.Command {
	c := &cobra.Command{Use: "task", Short: "Maintenance tasks"}
	c.AddCommand(&cobra.Command{Use: "ls", Short: "List tasks", RunE: func(cmd *cobra.Command, args []string) error {
		var ts []map[string]any
		if err := do("GET", "/api/v1/tasks", nil, &ts); err != nil {
			return err
		}
		if output == "json" {
			printJSON(ts)
			return nil
		}
		var rows [][]string
		for _, t := range ts {
			rows = append(rows, []string{fmt.Sprint(t["name"]), fmt.Sprint(t["interval"]), fmt.Sprint(t["lastRun"]), fmt.Sprint(t["running"]), fmt.Sprint(t["description"])})
		}
		table([]string{"NAME", "INTERVAL", "LAST RUN", "RUNNING", "DESCRIPTION"}, rows)
		return nil
	}})
	c.AddCommand(&cobra.Command{Use: "run <name>", Short: "Run a task now", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := do("POST", "/api/v1/tasks/"+args[0]+"/run", nil, nil); err != nil {
			return err
		}
		fmt.Println(T("task.started", args[0]))
		return nil
	}})
	c.AddCommand(&cobra.Command{Use: "runs", Short: "Recent task runs", RunE: func(cmd *cobra.Command, args []string) error {
		var rs []map[string]any
		if err := do("GET", "/api/v1/tasks/runs", nil, &rs); err != nil {
			return err
		}
		if output == "json" {
			printJSON(rs)
			return nil
		}
		var rows [][]string
		for _, r := range rs {
			rows = append(rows, []string{fmt.Sprint(r["taskName"]), fmt.Sprint(r["status"]), fmt.Sprint(r["startedAt"]), strings.TrimSpace(fmt.Sprint(r["log"]))})
		}
		table([]string{"TASK", "STATUS", "STARTED", "LOG"}, rows)
		return nil
	}})
	return c
}

func human(v any) string {
	f, _ := v.(float64)
	switch {
	case f >= 1<<30:
		return fmt.Sprintf("%.1f GB", f/(1<<30))
	case f >= 1<<20:
		return fmt.Sprintf("%.1f MB", f/(1<<20))
	case f >= 1<<10:
		return fmt.Sprintf("%.1f KB", f/(1<<10))
	}
	return fmt.Sprintf("%.0f B", f)
}
