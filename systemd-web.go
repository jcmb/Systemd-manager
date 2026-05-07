package main

import (
	"flag"
	"html/template"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

// ServiceData holds the information passed to the HTML template
type ServiceData struct {
	Name        string
	Active      string
	Sub         string
	Description string
	Class       string
}

// Common CSS/JS injected into all templates to handle themes
const themeAssets = `
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<script>
		// Run this immediately in the <head> to prevent a flash of unstyled content
		const savedTheme = localStorage.getItem('theme') || 'system';
		if (savedTheme === 'dark') {
			document.documentElement.setAttribute('data-theme', 'dark');
		} else if (savedTheme === 'light') {
			document.documentElement.setAttribute('data-theme', 'light');
		}
	</script>
	<style>
		:root {
			--bg-color: #f4f4f9;
			--text-color: #333;
			--table-bg: white;
			--table-border: #ddd;
			--th-bg: #f8f9fa;
			--th-hover: #e9ecef;
			--btn-bg: #fff;
			--btn-border: #ccc;
			--btn-hover: #eee;
			--link-color: #007bff;
			--pre-bg: #f8f9fa;
			--pre-border: #ddd;
			--c-running: #28a745;
			--c-failed: #dc3545;
			--c-disabled: #6c757d;
			--c-notfound: #fd7e14;
		}

		/* Dark Mode Variables (Tokyo Night Inspired) */
		[data-theme="dark"] {
			--bg-color: #1a1b26;
			--text-color: #a9b1d6;
			--table-bg: #24283b;
			--table-border: #414868;
			--th-bg: #292e42;
			--th-hover: #3b4261;
			--btn-bg: #24283b;
			--btn-border: #414868;
			--btn-hover: #3b4261;
			--link-color: #7aa2f7;
			--pre-bg: #1f2335;
			--pre-border: #414868;
			--c-running: #9ece6a;
			--c-failed: #f7768e;
			--c-disabled: #565f89;
			--c-notfound: #ff9e64;
		}

		/* System Default Fallback */
		@media (prefers-color-scheme: dark) {
			:root:not([data-theme="light"]) {
				--bg-color: #1a1b26;
				--text-color: #a9b1d6;
				--table-bg: #24283b;
				--table-border: #414868;
				--th-bg: #292e42;
				--th-hover: #3b4261;
				--btn-bg: #24283b;
				--btn-border: #414868;
				--btn-hover: #3b4261;
				--link-color: #7aa2f7;
				--pre-bg: #1f2335;
				--pre-border: #414868;
				--c-running: #9ece6a;
				--c-failed: #f7768e;
				--c-disabled: #565f89;
				--c-notfound: #ff9e64;
			}
		}

		body { font-family: sans-serif; margin: 20px; background: var(--bg-color); color: var(--text-color); transition: background-color 0.2s, color 0.2s; }
		a { text-decoration: none; color: var(--link-color); }
		a:hover { text-decoration: underline; }
		a.mono { font-family: monospace; }
		
		.header-bar { display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid var(--table-border); padding-bottom: 10px; margin-bottom: 20px; }
		.header-bar h2 { margin: 0; }
		.theme-selector { display: flex; align-items: center; gap: 10px; font-size: 14px; }
		.theme-selector select { padding: 4px 8px; border-radius: 4px; border: 1px solid var(--btn-border); background: var(--btn-bg); color: var(--text-color); cursor: pointer; }

		button { padding: 6px 10px; cursor: pointer; border: 1px solid var(--btn-border); background: var(--btn-bg); color: var(--text-color); border-radius: 4px; margin-right: 2px; }
		button:hover { background: var(--btn-hover); }

		/* Status Colors */
		.running { color: var(--c-running); font-weight: bold; }
		.failed { color: var(--c-failed); font-weight: bold; }
		.disabled { color: var(--c-disabled); font-weight: bold; }
		.not-found { color: var(--c-notfound); font-weight: bold; }
	</style>
`

const themeSwitcherJS = `
	<script>
		document.getElementById('theme-select').value = localStorage.getItem('theme') || 'system';
		function changeTheme(theme) {
			localStorage.setItem('theme', theme);
			if (theme === 'system') {
				document.documentElement.removeAttribute('data-theme');
			} else {
				document.documentElement.setAttribute('data-theme', theme);
			}
		}
	</script>
`

// HTML Templates
const dashboardTemplate = `
<!DOCTYPE html>
<html>
<head>
	<title>Systemd Manager</title>
	` + themeAssets + `
	<style>
		table { border-collapse: collapse; width: 100%; background: var(--table-bg); box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
		th, td { text-align: left; padding: 12px; border-bottom: 1px solid var(--table-border); }
		th { background-color: var(--th-bg); cursor: pointer; user-select: none; }
		th:hover { background-color: var(--th-hover); }
	</style>
</head>
<body>
	<div class="header-bar">
		<h2>Embedded Systemd Manager</h2>
		<div class="theme-selector">
			<label for="theme-select">Theme:</label>
			<select id="theme-select" onchange="changeTheme(this.value)">
				<option value="system">System Default</option>
				<option value="light">Light</option>
				<option value="dark">Dark</option>
			</select>
		</div>
	</div>

	<table id="serviceTable">
		<thead>
			<tr>
				<th onclick="sortTable(0)">Service Name &#x21D5;</th>
				<th onclick="sortTable(1)">Status &#x21D5;</th>
				<th onclick="sortTable(2)">State &#x21D5;</th>
				<th onclick="sortTable(3)">Description &#x21D5;</th>
				<th>Controls</th>
			</tr>
		</thead>
		<tbody>
			{{range .}}
			<tr>
				<td><a class="mono" href="/status?service={{.Name}}">{{.Name}}</a></td>
				<td class="{{.Class}}">{{.Active}}</td>
				<td>{{.Sub}}</td>
				<td>{{.Description}}</td>
				<td style="white-space: nowrap;">
					<form method="POST" action="/action" style="display:inline;">
						<input type="hidden" name="service" value="{{.Name}}">
						<button type="submit" name="action" value="start">Start</button>
						<button type="submit" name="action" value="stop">Stop</button>
						<button type="submit" name="action" value="restart">Restart</button>
					</form>
					<form method="GET" action="/dependencies" style="display:inline;">
						<input type="hidden" name="service" value="{{.Name}}">
						<button type="submit">Dependencies</button>
					</form>
				</td>
			</tr>
			{{end}}
		</tbody>
	</table>

	<script>
		function sortTable(n) {
			var table, rows, switching, i, x, y, shouldSwitch, dir, switchcount = 0;
			table = document.getElementById("serviceTable");
			switching = true;
			dir = "asc"; 
			while (switching) {
				switching = false;
				rows = table.rows;
				for (i = 1; i < (rows.length - 1); i++) {
					shouldSwitch = false;
					x = rows[i].getElementsByTagName("TD")[n];
					y = rows[i + 1].getElementsByTagName("TD")[n];
					if (dir == "asc") {
						if (x.innerHTML.toLowerCase() > y.innerHTML.toLowerCase()) { shouldSwitch = true; break; }
					} else {
						if (x.innerHTML.toLowerCase() < y.innerHTML.toLowerCase()) { shouldSwitch = true; break; }
					}
				}
				if (shouldSwitch) {
					rows[i].parentNode.insertBefore(rows[i + 1], rows[i]);
					switching = true;
					switchcount ++;      
				} else {
					if (switchcount == 0 && dir == "asc") {
						dir = "desc";
						switching = true;
					}
				}
			}
		}
	</script>
	` + themeSwitcherJS + `
</body>
</html>
`

const statusTemplate = `
<!DOCTYPE html>
<html>
<head>
	<title>Status: {{.Name}}</title>
	` + themeAssets + `
	<style>
		pre { background: var(--pre-bg); padding: 20px; border-radius: 5px; overflow-x: auto; white-space: pre-wrap; word-wrap: break-word; border: 1px solid var(--pre-border); font-family: monospace; font-size: 14px;}
		h3 { margin-top: 30px; color: var(--link-color); border-bottom: 1px solid var(--table-border); padding-bottom: 5px; }
	</style>
</head>
<body>
	<div class="header-bar">
		<div>
			<a href="/" style="font-size: 16px; margin-bottom: 8px; display: inline-block;">&#8592; Back to Dashboard</a>
			<h2>Detailed Information for {{.Name}}</h2>
		</div>
		<div class="theme-selector">
			<label for="theme-select">Theme:</label>
			<select id="theme-select" onchange="changeTheme(this.value)">
				<option value="system">System Default</option>
				<option value="light">Light</option>
				<option value="dark">Dark</option>
			</select>
		</div>
	</div>
	
	<h3>Live Status & Recent Logs</h3>
	<pre>{{.Output}}</pre>

	<h3>Service Configuration File</h3>
	<pre>{{.Config}}</pre>
	
	` + themeSwitcherJS + `
</body>
</html>
`

const dependenciesTemplate = `
<!DOCTYPE html>
<html>
<head>
	<title>Dependencies: {{.Name}}</title>
	` + themeAssets + `
	<style>
		pre { background: var(--pre-bg); padding: 20px; border-radius: 5px; overflow-x: auto; white-space: pre; border: 1px solid var(--pre-border); font-family: monospace; font-size: 14px;}
	</style>
</head>
<body>
	<div class="header-bar">
		<div>
			<a href="/" style="font-size: 16px; margin-bottom: 8px; display: inline-block;">&#8592; Back to Dashboard</a>
			<h2>Reverse Dependencies for {{.Name}}</h2>
		</div>
		<div class="theme-selector">
			<label for="theme-select">Theme:</label>
			<select id="theme-select" onchange="changeTheme(this.value)">
				<option value="system">System Default</option>
				<option value="light">Light</option>
				<option value="dark">Dark</option>
			</select>
		</div>
	</div>
	
	<p>This tree shows which other units on your system are currently asking systemd to load <strong>{{.Name}}</strong>.</p>
	<pre>{{.Output}}</pre>
	
	` + themeSwitcherJS + `
</body>
</html>
`

// Handlers and Security
func isValidServiceName(name string) bool {
	if !strings.HasSuffix(name, ".service") {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '@' || c == '.') {
			return false
		}
	}
	return true
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	cmd := exec.Command("systemctl", "list-units", "--type=service", "--all", "--no-pager", "--no-legend")
	out, err := cmd.Output()
	if err != nil {
		http.Error(w, "Failed to fetch services", http.StatusInternalServerError)
		return
	}

	lines := strings.Split(string(out), "\n")
	var services []ServiceData

	for _, line := range lines {
		line = strings.ReplaceAll(line, "●", "")
		line = strings.ReplaceAll(line, "*", "")
		line = strings.TrimSpace(line)
		
		if line == "" {
			continue
		}
		
		fields := strings.Fields(line)
		if len(fields) >= 4 {
			name := fields[0]
			load := fields[1]
			active := fields[2]
			sub := fields[3]
			desc := ""
			if len(fields) > 4 {
				desc = strings.Join(fields[4:], " ")
			}

			class := "disabled"
			if active == "active" {
				class = "running"
			} else if active == "failed" {
				class = "failed"
			} else if load == "not-found" {
				class = "not-found"
			}

			services = append(services, ServiceData{
				Name: name, Active: active, Sub: sub, Description: desc, Class: class,
			})
		}
	}

	t, err := template.New("index").Parse(dashboardTemplate)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	t.Execute(w, services)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")

	if !isValidServiceName(service) {
		http.Error(w, "Invalid service name", http.StatusBadRequest)
		return
	}

	cmdStatus := exec.Command("systemctl", "status", service, "--no-pager")
	outStatus, _ := cmdStatus.CombinedOutput()

	cmdCat := exec.Command("systemctl", "cat", service)
	outCat, _ := cmdCat.CombinedOutput()
	
	configStr := string(outCat)
	if configStr == "" {
		configStr = "No configuration file found for this service."
	}

	data := struct {
		Name   string
		Output string
		Config string
	}{
		Name:   service,
		Output: string(outStatus),
		Config: configStr,
	}

	t, err := template.New("status").Parse(statusTemplate)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	t.Execute(w, data)
}

func dependenciesHandler(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")

	if !isValidServiceName(service) {
		http.Error(w, "Invalid service name", http.StatusBadRequest)
		return
	}

	cmd := exec.Command("systemctl", "list-dependencies", "--reverse", service, "--no-pager")
	out, _ := cmd.CombinedOutput()

	outputStr := string(out)
	if outputStr == "" {
		outputStr = "No reverse dependencies found."
	}

	data := struct {
		Name   string
		Output string
	}{
		Name:   service,
		Output: outputStr,
	}

	t, err := template.New("dependencies").Parse(dependenciesTemplate)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	t.Execute(w, data)
}

func actionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	service := r.FormValue("service")
	action := r.FormValue("action")

	validActions := map[string]bool{"start": true, "stop": true, "restart": true}

	if isValidServiceName(service) && validActions[action] {
		cmd := exec.Command("sudo", "systemctl", action, service)
		err := cmd.Run()
		if err != nil {
			log.Printf("Error executing %s on %s: %v\n", action, service, err)
		}
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func main() {
	bind := flag.String("bind", "", "host or IP to listen on (overrides -address if set)")
	address := flag.String("address", "0.0.0.0", "host or IP to listen on (use -bind for the same; both kept for compatibility)")
	port := flag.Int("port", 6999, "TCP port to listen on")
	flag.Parse()

	listenHost := *address
	if *bind != "" {
		listenHost = *bind
	}
	bindAddr := net.JoinHostPort(listenHost, strconv.Itoa(*port))

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/status", statusHandler)
	http.HandleFunc("/dependencies", dependenciesHandler)
	http.HandleFunc("/action", actionHandler)

	log.Printf("Starting Embedded Systemd Manager on %s...\n", bindAddr)
	log.Fatal(http.ListenAndServe(bindAddr, nil))
}