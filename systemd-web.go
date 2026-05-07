package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// version is set at link time by build.sh: -X main.version=...
var version = "dev"

// ServiceData holds the information passed to the HTML template
type ServiceData struct {
	Name        string
	Active      string
	Sub         string
	Description string
	Class       string
	// Extended metrics (from systemctl show)
	Enabled      string
	Uptime       string
	UptimeSort   int64 // seconds since active (for sorting); -1 if n/a
	Restarts     string
	RestartsSort int
	Memory       string // human display
	MemCurSort   uint64
	MemPeakSort  uint64
	Tasks        string
	TasksSort    int
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
		.table-wrap { overflow-x: auto; margin-bottom: 20px; }
		table { border-collapse: collapse; width: 100%; min-width: 1100px; background: var(--table-bg); box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
		th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--table-border); font-size: 14px; }
		th { background-color: var(--th-bg); cursor: pointer; user-select: none; white-space: nowrap; }
		th:hover { background-color: var(--th-hover); }
		td.mono-sm { font-family: monospace; font-size: 13px; }
		td.num { text-align: right; font-variant-numeric: tabular-nums; }
		th.num { text-align: right; }
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

	<div class="table-wrap">
	<table id="serviceTable">
		<thead>
			<tr>
				<th onclick="sortTable(0)">Service &#x21D5;</th>
				<th onclick="sortTable(1)">Status &#x21D5;</th>
				<th onclick="sortTable(2)">State &#x21D5;</th>
				<th onclick="sortTable(3)">Uptime &#x21D5;</th>
				<th class="num" onclick="sortTable(4)">Restarts &#x21D5;</th>
				<th onclick="sortTable(5)">Memory (cur / peak) &#x21D5;</th>
				<th onclick="sortTable(6)">Enabled &#x21D5;</th>
				<th class="num" onclick="sortTable(7)">Tasks &#x21D5;</th>
				<th onclick="sortTable(8)">Description &#x21D5;</th>
				<th>Controls</th>
			</tr>
		</thead>
		<tbody>
			{{range .}}
			<tr>
				<td><a class="mono" href="/status?service={{.Name}}">{{.Name}}</a></td>
				<td class="{{.Class}}">{{.Active}}</td>
				<td>{{.Sub}}</td>
				<td class="mono-sm" data-sort="{{.UptimeSort}}">{{.Uptime}}</td>
				<td class="num" data-sort="{{.RestartsSort}}">{{.Restarts}}</td>
				<td class="mono-sm" data-sort="{{.MemCurSort}}">{{.Memory}}</td>
				<td data-sort="{{.Enabled}}">{{.Enabled}}</td>
				<td class="num" data-sort="{{.TasksSort}}">{{.Tasks}}</td>
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
						<button type="submit">Deps</button>
					</form>
				</td>
			</tr>
			{{end}}
		</tbody>
	</table>
	</div>

	<script>
		function cellSortVal(td) {
			var ds = td.getAttribute("data-sort");
			if (ds !== null && ds !== "") {
				var n = parseFloat(ds, 10);
				if (!isNaN(n)) return { t: "n", v: n, s: ds };
				return { t: "s", v: (ds + "").toLowerCase(), s: ds };
			}
			return { t: "s", v: td.textContent.trim().toLowerCase(), s: "" };
		}
		function cmpCell(a, b) {
			if (a.t === "n" && b.t === "n") return a.v - b.v;
			if (a.t === "n") return -1;
			if (b.t === "n") return 1;
			if (a.v < b.v) return -1;
			if (a.v > b.v) return 1;
			return 0;
		}
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
					var cx = cellSortVal(x), cy = cellSortVal(y);
					if (dir == "asc") {
						if (cmpCell(cx, cy) > 0) { shouldSwitch = true; break; }
					} else {
						if (cmpCell(cx, cy) < 0) { shouldSwitch = true; break; }
					}
				}
				if (shouldSwitch) {
					rows[i].parentNode.insertBefore(rows[i + 1], rows[i]);
					switching = true;
					switchcount++;
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

const systemctlShowChunk = 40

func parseSystemctlShowBlocks(data string) []map[string]string {
	var blocks []map[string]string
	cur := make(map[string]string)
	flush := func() {
		if len(cur) == 0 {
			return
		}
		blocks = append(blocks, cur)
		cur = make(map[string]string)
	}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key, val := line[:idx], line[idx+1:]
		if key == "Id" {
			if _, ok := cur["Id"]; ok {
				flush()
			}
		}
		cur[key] = val
	}
	flush()
	return blocks
}

func fetchServiceProperties(names []string) map[string]map[string]string {
	out := make(map[string]map[string]string)
	for i := 0; i < len(names); i += systemctlShowChunk {
		end := i + systemctlShowChunk
		if end > len(names) {
			end = len(names)
		}
		chunk := names[i:end]
		args := []string{
			"show", "--no-pager",
			"-p", "Id",
			"-p", "ActiveState",
			"-p", "SubState",
			"-p", "ActiveEnterTimestamp",
			"-p", "NRestarts",
			"-p", "MemoryCurrent",
			"-p", "MemoryPeak",
			"-p", "UnitFileState",
			"-p", "TasksCurrent",
			"-p", "TasksMax",
		}
		args = append(args, chunk...)
		cmd := exec.Command("systemctl", args...)
		data, err := cmd.Output()
		if err != nil {
			log.Printf("systemctl show batch %d-%d: %v", i, end, err)
			continue
		}
		for _, m := range parseSystemctlShowBlocks(string(data)) {
			if id := strings.TrimSpace(m["Id"]); id != "" {
				out[id] = m
			}
		}
	}
	return out
}

func shortUnitFileState(s string) string {
	s = strings.TrimSpace(s)
	switch s {
	case "enabled", "enabled-runtime":
		return "yes"
	case "disabled":
		return "no"
	case "static":
		return "static"
	case "indirect":
		return "indirect"
	case "alias":
		return "alias"
	case "generated":
		return "gen"
	case "transient":
		return "trans"
	case "linked", "linked-runtime":
		return "linked"
	case "masked", "masked-runtime":
		return "masked"
	default:
		if s == "" {
			return "—"
		}
		return s
	}
}

func parseByteProp(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "[not set]") {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func formatBytesIEC(b uint64) string {
	if b == 0 {
		return "0 B"
	}
	v := float64(b)
	const k = 1024
	unit := 0
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	for v >= k && unit < len(units)-1 {
		v /= k
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d B", b)
	}
	return fmt.Sprintf("%.1f %s", v, units[unit])
}

func parseActiveEnterTimestamp(val string) (time.Time, bool) {
	val = strings.TrimSpace(val)
	if val == "" || strings.EqualFold(val, "n/a") {
		return time.Time{}, false
	}
	layouts := []string{
		"Mon 2006-01-02 15:04:05 MST",
		"Mon 2006-01-02 15:04:05 Z07:00",
		"Mon Jan 2 15:04:05 MST 2006",
		"Mon Jan _2 15:04:05 MST 2006",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, val, time.Local); err == nil {
			return t, true
		}
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, val); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func humanizeDurationSince(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	sec := int64(d / time.Second)
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	m := sec / 60
	sec %= 60
	if m < 60 {
		return fmt.Sprintf("%dm %ds", m, sec)
	}
	h := m / 60
	m %= 60
	if h < 48 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	days := h / 24
	h %= 24
	return fmt.Sprintf("%dd %dh", days, h)
}

func formatTasks(cur, max string) (display string, sortN int) {
	cur = strings.TrimSpace(cur)
	max = strings.TrimSpace(max)
	if cur == "" || strings.EqualFold(cur, "[not set]") {
		return "—", -1
	}
	n, err := strconv.Atoi(cur)
	if err != nil {
		return "—", -1
	}
	if max == "" || strings.EqualFold(max, "[not set]") || strings.EqualFold(max, "infinity") {
		return cur, n
	}
	return cur + " / " + max, n
}

func applyServiceProps(row *ServiceData, p map[string]string) {
	if len(p) == 0 {
		row.Enabled = "—"
		row.Uptime = "—"
		row.UptimeSort = -1
		row.Restarts = "—"
		row.RestartsSort = 0
		row.Memory = "—"
		row.MemCurSort = 0
		row.MemPeakSort = 0
		row.Tasks = "—"
		row.TasksSort = -1
		return
	}

	row.Enabled = shortUnitFileState(p["UnitFileState"])

	active := strings.TrimSpace(p["ActiveState"])
	if active == "active" {
		if t, ok := parseActiveEnterTimestamp(p["ActiveEnterTimestamp"]); ok {
			row.Uptime = humanizeDurationSince(t)
			row.UptimeSort = int64(time.Since(t).Seconds())
		} else {
			row.Uptime = "—"
			row.UptimeSort = -1
		}
	} else {
		row.Uptime = "—"
		row.UptimeSort = -1
	}

	nr, hasNR := p["NRestarts"]
	nr = strings.TrimSpace(nr)
	if !hasNR || nr == "" || strings.EqualFold(nr, "[not set]") {
		row.Restarts = "—"
		row.RestartsSort = -1
	} else if n, err := strconv.Atoi(nr); err == nil {
		row.Restarts = nr
		row.RestartsSort = n
	} else {
		row.Restarts = nr
		row.RestartsSort = 0
	}

	mc, mcOk := parseByteProp(p["MemoryCurrent"])
	mp, mpOk := parseByteProp(p["MemoryPeak"])
	row.MemCurSort = mc
	row.MemPeakSort = mp
	if !mcOk && !mpOk {
		row.Memory = "—"
	} else {
		curS, peakS := "—", "—"
		if mcOk {
			curS = formatBytesIEC(mc)
		}
		if mpOk {
			peakS = formatBytesIEC(mp)
		}
		row.Memory = curS + " / " + peakS
	}

	row.Tasks, row.TasksSort = formatTasks(p["TasksCurrent"], p["TasksMax"])
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
	var names []string

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

			if !isValidServiceName(name) {
				continue
			}

			class := "disabled"
			if active == "active" {
				class = "running"
			} else if active == "failed" {
				class = "failed"
			} else if load == "not-found" {
				class = "not-found"
			}

			names = append(names, name)
			services = append(services, ServiceData{
				Name: name, Active: active, Sub: sub, Description: desc, Class: class,
			})
		}
	}

	props := fetchServiceProperties(names)
	for i := range services {
		if p, ok := props[services[i].Name]; ok {
			applyServiceProps(&services[i], p)
		} else {
			applyServiceProps(&services[i], nil)
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
	showVersion := flag.Bool("version", false, "print version and exit")
	bind := flag.String("bind", "127.0.0.1", "host or IP to listen on")
	port := flag.Int("port", 6999, "TCP port to listen on")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	listenHost := strings.TrimSpace(*bind)
	if listenHost == "" {
		listenHost = "127.0.0.1"
	}
	bindAddr := net.JoinHostPort(listenHost, strconv.Itoa(*port))

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/status", statusHandler)
	http.HandleFunc("/dependencies", dependenciesHandler)
	http.HandleFunc("/action", actionHandler)

	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", bindAddr, err)
	}
	log.Printf("Starting systemd-web %s on %s\n", version, ln.Addr().String())
	log.Fatal(http.Serve(ln, nil))
}