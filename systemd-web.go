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
	Enabled       string
	Uptime        string
	UptimeSort    int64 // seconds since active (for sorting); -1 if n/a
	Restarts      string
	RestartsSort  int
	MemCurrent    string  // MiB
	MemPeak       string  // MiB
	MemCurSort    float64 // MiB for sorting; -1 if n/a
	MemPeakSort   float64 // MiB for sorting; -1 if n/a
	Tasks         string
	TasksSort     int
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
		.site-top { position: sticky; top: 0; z-index: 100; display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 14px; margin-bottom: 16px; background: var(--th-bg); border: 1px solid var(--table-border); border-radius: 6px; box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
		.site-top .mono { font-family: monospace; font-size: 13px; }
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
	<title>Systemd Manager · {{.Version}}</title>
	` + themeAssets + `
	<style>
		.table-wrap { overflow-x: auto; margin-bottom: 20px; }
		table { border-collapse: collapse; width: 100%; min-width: 1380px; background: var(--table-bg); box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
		th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--table-border); font-size: 14px; }
		th { background-color: var(--th-bg); cursor: pointer; user-select: none; white-space: nowrap; }
		th:hover { background-color: var(--th-hover); }
		td.mono-sm { font-family: monospace; font-size: 13px; }
		td.num { text-align: right; font-variant-numeric: tabular-nums; }
		th.num { text-align: right; }
		.main-actions { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; margin: 0 0 16px; }
		.view-toggle { font-size: 14px; }
		.view-toggle a { margin-left: 6px; }
	</style>
</head>
<body>
	<!-- systemd-web {{.Version}} -->
	<div class="site-top">
		<span><strong>systemd-web</strong> <span class="mono">{{.Version}}</span></span>
		<form method="POST" action="/action" style="display:inline;">
			{{if .ShowAll}}<input type="hidden" name="redirect" value="all">{{end}}
			<button type="submit" name="action" value="daemon-reload">Daemon Reload</button>
		</form>
	</div>
	<div class="header-bar">
		<div>
			<h2>Embedded Systemd Manager</h2>
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

	<div class="main-actions">
		<span class="view-toggle">
			{{if .ShowAll}}
				Showing all {{len .Services}} services.
				<a href="/">Show filtered (running / enabled / static)</a>
			{{else}}
				Showing {{len .Services}} running / enabled / static services.
				<a href="/?all=1">Show all services</a>
			{{end}}
		</span>
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
				<th class="num" onclick="sortTable(5)">Mem cur (MiB) &#x21D5;</th>
				<th class="num" onclick="sortTable(6)">Mem peak (MiB) &#x21D5;</th>
				<th onclick="sortTable(7)">Enabled &#x21D5;</th>
				<th class="num" onclick="sortTable(8)">Tasks &#x21D5;</th>
				<th onclick="sortTable(9)">Description &#x21D5;</th>
				<th>Controls</th>
			</tr>
		</thead>
		<tbody>
			{{range .Services}}
			<tr>
				<td><a class="mono" href="/status?service={{.Name}}">{{.Name}}</a></td>
				<td class="{{.Class}}">{{.Active}}</td>
				<td>{{.Sub}}</td>
				<td class="mono-sm" data-sort="{{.UptimeSort}}">{{.Uptime}}</td>
				<td class="num" data-sort="{{.RestartsSort}}">{{.Restarts}}</td>
				<td class="num mono-sm" data-sort="{{printf "%.6f" .MemCurSort}}">{{.MemCurrent}}</td>
				<td class="num mono-sm" data-sort="{{printf "%.6f" .MemPeakSort}}">{{.MemPeak}}</td>
				<td data-sort="{{.Enabled}}">{{.Enabled}}</td>
				<td class="num" data-sort="{{.TasksSort}}">{{.Tasks}}</td>
				<td>{{.Description}}</td>
				<td style="white-space: nowrap;">
					<form method="POST" action="/action" style="display:inline;">
						<input type="hidden" name="service" value="{{.Name}}">
						{{if $.ShowAll}}<input type="hidden" name="redirect" value="all">{{end}}
						<button type="submit" name="action" value="start">Start</button>
						<button type="submit" name="action" value="stop">Stop</button>
						<button type="submit" name="action" value="restart">Restart</button>
						<button type="submit" name="action" value="enable">Enable</button>
						<button type="submit" name="action" value="disable">Disable</button>
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
	<title>Status: {{.Name}} · {{.Version}}</title>
	` + themeAssets + `
	<style>
		pre { background: var(--pre-bg); padding: 20px; border-radius: 5px; overflow-x: auto; white-space: pre-wrap; word-wrap: break-word; border: 1px solid var(--pre-border); font-family: monospace; font-size: 14px;}
		h3 { margin-top: 30px; color: var(--link-color); border-bottom: 1px solid var(--table-border); padding-bottom: 5px; }
		.service-controls { display: flex; flex-wrap: wrap; gap: 6px; margin: 10px 0 20px; align-items: center; }
		.service-controls form { display: inline; }
	</style>
</head>
<body>
	<!-- systemd-web {{.Version}} -->
	<div class="site-top">
		<span><strong>systemd-web</strong> <span class="mono">{{.Version}}</span></span>
		<form method="POST" action="/action" style="display:inline;">
			<input type="hidden" name="service" value="{{.Name}}">
			<input type="hidden" name="redirect" value="status">
			<button type="submit" name="action" value="daemon-reload">Daemon Reload</button>
		</form>
	</div>
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

	<div class="service-controls">
		<form method="POST" action="/action">
			<input type="hidden" name="service" value="{{.Name}}">
			<input type="hidden" name="redirect" value="status">
			<button type="submit" name="action" value="start">Start</button>
			<button type="submit" name="action" value="stop">Stop</button>
			<button type="submit" name="action" value="restart">Restart</button>
			<button type="submit" name="action" value="enable">Enable</button>
			<button type="submit" name="action" value="disable">Disable</button>
		</form>
		<form method="GET" action="/dependencies">
			<input type="hidden" name="service" value="{{.Name}}">
			<button type="submit">Deps</button>
		</form>
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
	<title>Dependencies: {{.Name}} · {{.Version}}</title>
	` + themeAssets + `
	<style>
		pre { background: var(--pre-bg); padding: 20px; border-radius: 5px; overflow-x: auto; white-space: pre; border: 1px solid var(--pre-border); font-family: monospace; font-size: 14px;}
	</style>
</head>
<body>
	<!-- systemd-web {{.Version}} -->
	<div class="site-top">
		<span><strong>systemd-web</strong> <span class="mono">{{.Version}}</span></span>
		<form method="POST" action="/action" style="display:inline;">
			<input type="hidden" name="service" value="{{.Name}}">
			<input type="hidden" name="redirect" value="deps">
			<button type="submit" name="action" value="daemon-reload">Daemon Reload</button>
		</form>
	</div>
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

const bytesPerMiB = 1024 * 1024

func formatMiB(bytes uint64, ok bool) string {
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%.2f MiB", float64(bytes)/bytesPerMiB)
}

func mibSort(bytes uint64, ok bool) float64 {
	if !ok {
		return -1
	}
	return float64(bytes) / bytesPerMiB
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

// servicePassesFilter keeps rows that are running now or are enabled / static at the unit-file level.
func servicePassesFilter(activeFromList string, p map[string]string) bool {
	if strings.TrimSpace(activeFromList) == "active" {
		return true
	}
	if p == nil {
		return false
	}
	switch strings.TrimSpace(p["UnitFileState"]) {
	case "enabled", "enabled-runtime", "static":
		return true
	default:
		return false
	}
}

func applyServiceProps(row *ServiceData, p map[string]string) {
	if len(p) == 0 {
		row.Enabled = "—"
		row.Uptime = "—"
		row.UptimeSort = -1
		row.Restarts = "—"
		row.RestartsSort = 0
		row.MemCurrent = "—"
		row.MemPeak = "—"
		row.MemCurSort = -1
		row.MemPeakSort = -1
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
	row.MemCurrent = formatMiB(mc, mcOk)
	row.MemPeak = formatMiB(mp, mpOk)
	row.MemCurSort = mibSort(mc, mcOk)
	row.MemPeakSort = mibSort(mp, mpOk)

	row.Tasks, row.TasksSort = formatTasks(p["TasksCurrent"], p["TasksMax"])
}

// setHTMLResponseHeaders avoids stale HTML from proxies or embedded browsers and
// exposes the build id for checks that skip the document body (e.g. curl -I).
func setHTMLResponseHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store, max-age=0, must-revalidate")
	h.Set("Pragma", "no-cache")
	h.Set("X-Application-Version", version)
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

	showAll := r.URL.Query().Get("all") == "1"
	if !showAll {
		filtered := services[:0]
		for i := range services {
			p := props[services[i].Name]
			if servicePassesFilter(services[i].Active, p) {
				filtered = append(filtered, services[i])
			}
		}
		services = filtered
	}

	t, err := template.New("index").Parse(dashboardTemplate)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	setHTMLResponseHeaders(w)
	if err := t.Execute(w, struct {
		Services []ServiceData
		ShowAll  bool
		Version  string
	}{Services: services, ShowAll: showAll, Version: version}); err != nil {
		log.Printf("template index: %v", err)
	}
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
		Name    string
		Output  string
		Config  string
		Version string
	}{
		Name:    service,
		Output:  string(outStatus),
		Config:  configStr,
		Version: version,
	}

	t, err := template.New("status").Parse(statusTemplate)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	setHTMLResponseHeaders(w)
	if err := t.Execute(w, data); err != nil {
		log.Printf("template status: %v", err)
	}
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
		Name    string
		Output  string
		Version string
	}{
		Name:    service,
		Output:  outputStr,
		Version: version,
	}

	t, err := template.New("dependencies").Parse(dependenciesTemplate)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	setHTMLResponseHeaders(w)
	if err := t.Execute(w, data); err != nil {
		log.Printf("template dependencies: %v", err)
	}
}

func actionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	service := r.FormValue("service")
	action := r.FormValue("action")
	redirect := r.FormValue("redirect")

	validActions := map[string]bool{
		"start":   true,
		"stop":    true,
		"restart": true,
		"enable":  true,
		"disable": true,
	}

	switch {
	case action == "daemon-reload":
		cmd := exec.Command("sudo", "systemctl", "daemon-reload")
		if err := cmd.Run(); err != nil {
			log.Printf("Error executing daemon-reload: %v\n", err)
		}
	case isValidServiceName(service) && validActions[action]:
		cmd := exec.Command("sudo", "systemctl", action, service)
		if err := cmd.Run(); err != nil {
			log.Printf("Error executing %s on %s: %v\n", action, service, err)
		}
	}

	target := "/"
	switch {
	case redirect == "status" && isValidServiceName(service):
		target = "/status?service=" + service
	case redirect == "deps" && isValidServiceName(service):
		target = "/dependencies?service=" + service
	case redirect == "all":
		target = "/?all=1"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	bind := flag.String("bind", "127.0.0.1", "host or IP to listen on")
	port := flag.Int("port", 7002, "TCP port to listen on")
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