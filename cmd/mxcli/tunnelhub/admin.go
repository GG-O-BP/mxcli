// SPDX-License-Identifier: Apache-2.0

package tunnelhub

import "net/http"

// NewAdmin returns the handler for the hub's admin pages: the preview overview at
// "/" and the "get your CLI key" page at "/cli". Both are self-contained (inline
// CSS/JS, no external assets). When auth is on, the server gates these behind a
// session before they are reached.
func NewAdmin(_ *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(adminHTML))
		case "/cli":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(cliKeyHTML))
		default:
			http.NotFound(w, r)
		}
	})
}

const adminHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>mxcli tunnel-hub — previews</title>
<style>
  :root { color-scheme: light dark; --bg:#fff; --fg:#1a1a2e; --mut:#6b7280; --line:#e5e7eb; --row:#f9fafb; --accent:#3b3bd6; }
  @media (prefers-color-scheme: dark){ :root{ --bg:#0f1020; --fg:#e6e6f0; --mut:#9aa0b4; --line:#26283b; --row:#171a2e; --accent:#8f8fff; } }
  * { box-sizing:border-box; }
  body { font:15px/1.45 system-ui,sans-serif; margin:0; background:var(--bg); color:var(--fg); }
  header { padding:1.1rem 1.4rem; border-bottom:1px solid var(--line); display:flex; align-items:baseline; gap:.8rem; flex-wrap:wrap; }
  h1 { font-size:1.05rem; margin:0; font-weight:650; }
  .meta { color:var(--mut); font-size:.85rem; }
  .wrap { padding:1rem 1.4rem; overflow-x:auto; }
  table { border-collapse:collapse; width:100%; min-width:52rem; }
  th,td { text-align:left; padding:.5rem .7rem; border-bottom:1px solid var(--line); white-space:nowrap; }
  th { font-size:.72rem; text-transform:uppercase; letter-spacing:.04em; color:var(--mut); cursor:pointer; user-select:none; }
  th.sorted::after { content:" \25BC"; font-size:.7em; }
  tbody tr:nth-child(even){ background:var(--row); }
  td.url a { color:var(--accent); text-decoration:none; }
  td.url a:hover { text-decoration:underline; }
  .dot { display:inline-block; width:.62rem; height:.62rem; border-radius:50%; margin-right:.4rem; vertical-align:-1px; }
  .available .dot { background:#22c55e; } .stale .dot { background:#f59e0b; }
  .available { color:inherit; } .stale { color:var(--mut); }
  .sol { color:var(--mut); font-size:.82rem; }
  .empty { color:var(--mut); padding:2rem 0; }
  code { font-family:ui-monospace,monospace; }
  .who { color:var(--mut); font-size:.85rem; }
  .who b { color:var(--fg); font-weight:600; }
  .signout { font:inherit; font-size:.82rem; color:var(--accent); background:none; border:1px solid var(--line); border-radius:.4rem; padding:.15rem .55rem; cursor:pointer; }
  .signout:hover { border-color:var(--accent); }
</style></head>
<body>
<header>
  <h1>mxcli tunnel-hub</h1>
  <span class="meta" id="count">…</span>
  <span class="who" id="who" style="margin-left:auto"></span>
  <a class="signout" id="clikey" href="/cli" hidden style="text-decoration:none">Get CLI key</a>
  <button class="signout" id="signout" hidden>Sign out</button>
  <span class="meta" id="updated"></span>
</header>
<div class="wrap">
  <table>
    <thead><tr>
      <th data-k="availability">Status</th>
      <th data-k="solution">Solution</th>
      <th data-k="project">Project</th>
      <th data-k="branch">Branch</th>
      <th data-k="url">URL</th>
      <th data-k="registeredAt">Registered</th>
      <th data-k="lastSeenAt">Last seen</th>
      <th data-k="lastUsedAt">Last used</th>
      <th data-k="uptimeSec">Uptime</th>
    </tr></thead>
    <tbody id="rows"></tbody>
  </table>
  <div class="empty" id="empty" hidden>No previews registered yet. Start one with <code>mxcli run --hub …</code></div>
</div>
<script>
(function(){
  var sortKey = "lastUsedAt", data = [];
  function ago(t){
    if(!t || t.startsWith("0001")) return "—";
    var s = Math.max(0,(Date.now()-new Date(t))/1000);
    if(s<60) return Math.floor(s)+"s ago";
    if(s<3600) return Math.floor(s/60)+"m ago";
    if(s<86400) return Math.floor(s/3600)+"h ago";
    return Math.floor(s/86400)+"d ago";
  }
  function dur(sec){
    if(sec<60) return sec+"s"; if(sec<3600) return Math.floor(sec/60)+"m";
    if(sec<86400) return Math.floor(sec/3600)+"h"; return Math.floor(sec/86400)+"d";
  }
  function cmp(a,b){
    if(sortKey==="project"){
      return (a.solution||"").localeCompare(b.solution||"") ||
             (a.project||"").localeCompare(b.project||"") ||
             (a.branch||"").localeCompare(b.branch||"");
    }
    if(sortKey==="availability") return (a.availability>b.availability?1:-1);
    if(sortKey==="uptimeSec") return b.uptimeSec-a.uptimeSec;
    // time keys: newest first
    return new Date(b[sortKey]||0)-new Date(a[sortKey]||0);
  }
  function esc(s){ var d=document.createElement("div"); d.textContent=s==null?"":s; return d.innerHTML; }
  function render(){
    data.sort(cmp);
    document.querySelectorAll("th").forEach(function(th){ th.classList.toggle("sorted", th.dataset.k===sortKey); });
    var rows = data.map(function(b){
      var name = (b.prefix?esc(b.prefix)+" · ":"")+esc(b.project);
      return "<tr class='"+esc(b.availability)+"'>"+
        "<td><span class='dot'></span>"+esc(b.availability)+"</td>"+
        "<td class='sol'>"+esc(b.solution||"—")+"</td>"+
        "<td>"+name+"</td>"+
        "<td><code>"+esc(b.branch||"—")+"</code></td>"+
        "<td class='url'><a href='"+esc(b.url)+"' target='_blank' rel='noopener'>"+esc(b.url.replace(/^https?:\/\//,""))+"</a></td>"+
        "<td>"+ago(b.registeredAt)+"</td>"+
        "<td>"+ago(b.lastSeenAt)+"</td>"+
        "<td>"+ago(b.lastUsedAt)+"</td>"+
        "<td>"+dur(b.uptimeSec)+"</td></tr>";
    }).join("");
    document.getElementById("rows").innerHTML = rows;
    document.getElementById("empty").hidden = data.length>0;
    document.getElementById("count").textContent = data.length+" preview"+(data.length===1?"":"s");
    document.getElementById("updated").textContent = "updated "+new Date().toLocaleTimeString();
  }
  function load(){
    fetch("/api/backends").then(function(r){return r.json();}).then(function(j){ data=j||[]; render(); }).catch(function(){});
  }
  function whoami(){
    fetch("/api/whoami").then(function(r){return r.json();}).then(function(j){
      var who = document.getElementById("who"), btn = document.getElementById("signout"),
          cli = document.getElementById("clikey");
      if(j && j.authEnabled && j.login){
        who.innerHTML = "signed in as <b>"+esc(j.login)+"</b>";
        btn.hidden = false; cli.hidden = false;
      } else {
        who.textContent = ""; btn.hidden = true; cli.hidden = true;
      }
    }).catch(function(){});
  }
  document.getElementById("signout").addEventListener("click", function(){
    fetch("/auth/logout", {method:"POST"}).then(function(){ location.reload(); }).catch(function(){ location.reload(); });
  });
  document.querySelectorAll("th").forEach(function(th){
    th.addEventListener("click", function(){ sortKey=th.dataset.k; render(); });
  });
  whoami(); load(); setInterval(load, 5000);
})();
</script>
</body></html>`

// cliKeyHTML is the "get your hub key" page (served at /cli). A signed-in user
// mints a durable hub key via the session cookie (no PAT, no device flow) and
// copies it into MXCLI_HUB_KEY. This is the primary key-acquisition path for a
// Claude Code web/mobile user, whose container cannot reach GitHub's device flow.
const cliKeyHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>mxcli tunnel-hub — CLI key</title>
<style>
  :root { color-scheme: light dark; --bg:#fff; --fg:#1a1a2e; --mut:#6b7280; --line:#e5e7eb; --row:#f9fafb; --accent:#3b3bd6; --ok:#22c55e; }
  @media (prefers-color-scheme: dark){ :root{ --bg:#0f1020; --fg:#e6e6f0; --mut:#9aa0b4; --line:#26283b; --row:#171a2e; --accent:#8f8fff; } }
  * { box-sizing:border-box; }
  body { font:15px/1.5 system-ui,sans-serif; margin:0; background:var(--bg); color:var(--fg); }
  header { padding:1.1rem 1.4rem; border-bottom:1px solid var(--line); display:flex; align-items:baseline; gap:.8rem; }
  h1 { font-size:1.05rem; margin:0; font-weight:650; }
  a { color:var(--accent); }
  .wrap { padding:1.5rem 1.4rem; max-width:44rem; }
  .who { color:var(--mut); font-size:.9rem; margin-bottom:1rem; }
  .who b { color:var(--fg); }
  button { font:inherit; font-weight:600; color:#fff; background:var(--accent); border:0; border-radius:.5rem; padding:.55rem 1rem; cursor:pointer; }
  button:disabled { opacity:.5; cursor:default; }
  .key { margin:1.2rem 0; padding:1rem; border:1px solid var(--line); border-radius:.6rem; background:var(--row); }
  .key code { font-family:ui-monospace,monospace; word-break:break-all; font-size:.95rem; }
  .copy { margin-left:.6rem; font-size:.8rem; padding:.2rem .6rem; background:none; color:var(--accent); border:1px solid var(--line); }
  pre { background:var(--row); border:1px solid var(--line); border-radius:.5rem; padding:.8rem 1rem; overflow-x:auto; font-size:.9rem; }
  .warn { color:var(--mut); font-size:.85rem; }
  .err { color:#ef4444; }
  code.inline { font-family:ui-monospace,monospace; background:var(--row); padding:.05rem .35rem; border-radius:.3rem; }
</style></head>
<body>
<header><h1>mxcli tunnel-hub</h1><a href="/">← previews</a></header>
<div class="wrap">
  <div class="who" id="who">…</div>
  <p>Create a durable hub key to let <code class="inline">mxcli run --hub</code> register previews as you.
     The key does not expire and survives hub restarts — you set it once.</p>
  <button id="mint">Create a hub key</button>
  <div id="out"></div>
</div>
<script>
(function(){
  function esc(s){ var d=document.createElement("div"); d.textContent=s==null?"":s; return d.innerHTML; }
  fetch("/api/whoami").then(function(r){return r.json();}).then(function(j){
    var who=document.getElementById("who");
    if(j && j.login){ who.innerHTML="Signed in as <b>"+esc(j.login)+"</b>."; }
    else { who.innerHTML="<span class='err'>Not signed in.</span> <a href='/auth/github/login?return="+encodeURIComponent(location.href)+"'>Sign in with GitHub</a>"; document.getElementById("mint").disabled=true; }
  });
  document.getElementById("mint").addEventListener("click", function(){
    var btn=this; btn.disabled=true; btn.textContent="Creating…";
    fetch("/api/keys", {method:"POST", headers:{"X-Hub-Mint":"1"}}).then(function(r){
      if(!r.ok) throw new Error("HTTP "+r.status);
      return r.json();
    }).then(function(j){
      var host=location.host;
      document.getElementById("out").innerHTML =
        "<div class='key'><code id='k'>"+esc(j.key)+"</code>"+
        "<button class='copy' id='cp'>Copy</button></div>"+
        "<p>Set it as an environment variable in your Claude Code environment (repo/environment secret):</p>"+
        "<pre>export MXCLI_HUB_KEY="+esc(j.key)+"</pre>"+
        "<p>Then, in any session:</p>"+
        "<pre>mxcli run --hub https://"+esc(host)+" -p app.mpr</pre>"+
        "<p class='warn'>Copy it now — it is shown only once. Creating another key does not revoke this one; "+
        "sign out or use <code class='inline'>mxcli auth hub logout</code> to revoke.</p>";
      document.getElementById("cp").addEventListener("click", function(){
        navigator.clipboard.writeText(j.key).then(function(){ this.textContent="Copied"; }.bind(this));
      });
      btn.textContent="Create another key"; btn.disabled=false;
    }).catch(function(e){
      document.getElementById("out").innerHTML="<p class='err'>Could not create a key ("+esc(e.message)+"). Try signing in again.</p>";
      btn.textContent="Create a hub key"; btn.disabled=false;
    });
  });
})();
</script>
</body></html>`
