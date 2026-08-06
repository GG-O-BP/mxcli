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

// Column layout for the per-session endpoint tables. The widths are declared
// here rather than derived from content so that a column is the same width in
// every session card — with table-layout:fixed the colgroup, not the longest
// cell, decides. URL takes the remainder (it is the column worth seeing in
// full); tableMinWidth is the sum of the fixed columns plus a usable share for
// it, and is applied to the card as well as the table so a narrow viewport
// scrolls every card together in .wrap.
const (
	tableMinWidth = "68rem"

	epColgroup = "<colgroup>" +
		"<col style='width:7.5rem'>" + // Status
		"<col style='width:18rem'>" + // Project
		"<col style='width:9rem'>" + // Branch
		"<col>" + // URL (flexible remainder)
		"<col style='width:6.5rem'>" + // First seen
		"<col style='width:6.5rem'>" + // Last seen
		"<col style='width:6.5rem'>" + // Last used
		"<col style='width:5rem'>" + // Uptime
		"</colgroup>"
)

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
  th,td { text-align:left; padding:.5rem .7rem; border-bottom:1px solid var(--line); white-space:nowrap;
          overflow:hidden; text-overflow:ellipsis; }
  th { font-size:.72rem; text-transform:uppercase; letter-spacing:.04em; color:var(--mut); cursor:pointer; user-select:none; }
  th.sorted::after { content:" \25BC"; font-size:.7em; }
  tbody tr:nth-child(even){ background:var(--row); }
  td.url a { color:var(--accent); text-decoration:none; }
  td.url a:hover { text-decoration:underline; }
  .dot { display:inline-block; width:.62rem; height:.62rem; border-radius:50%; margin-right:.4rem; vertical-align:-1px; background:var(--mut); }
  .dot.available { background:#22c55e; } .dot.stale { background:#f59e0b; } .dot.offline { background:#9ca3af; }
  tr.stale, tr.offline { color:var(--mut); }
  .sol { color:var(--mut); font-size:.82rem; }
  .empty { color:var(--mut); padding:2rem 0; }
  /* Every session card is the same width and its table is laid out from the
     colgroup below, so a column lines up across all cards regardless of how long
     the project names or URLs in any one card happen to be. The shared min-width
     keeps the cards aligned while .wrap scrolls them horizontally as a unit. */
  .ses { border:1px solid var(--line); border-radius:.6rem; margin-bottom:1rem; overflow:hidden; min-width:` + tableMinWidth + `; }
  .ses.offline { opacity:.72; }
  .sh { display:flex; align-items:baseline; gap:.7rem; flex-wrap:wrap; padding:.6rem .8rem; background:var(--row); border-bottom:1px solid var(--line); }
  .sh .sid { font-weight:600; font-family:ui-monospace,monospace; font-size:.9rem; }
  .sh .sid a { color:var(--accent); text-decoration:none; } .sh .sid a:hover { text-decoration:underline; }
  .sh .own { color:var(--mut); font-size:.85rem; } .sh .own::before { content:"@"; }
  .sh .cnt { color:var(--mut); font-size:.82rem; }
  .sh .fs { color:var(--mut); font-size:.82rem; margin-left:auto; }
  .sh .ls { color:var(--mut); font-size:.82rem; }
  .ses table { table-layout:fixed; min-width:` + tableMinWidth + `; }
  .ses td, .ses th { white-space:nowrap; }
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
  <div id="sessions"></div>
  <div class="empty" id="empty" hidden>No previews registered yet. Start one with <code>mxcli run --hub …</code></div>
</div>
<script>
(function(){
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
  function esc(s){ var d=document.createElement("div"); d.textContent=s==null?"":s; return d.innerHTML; }
  // esc() leaves quotes alone, so attribute values need their own escaper.
  function attr(s){
    return String(s==null?"":s).replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;")
      .replace(/"/g,"&quot;").replace(/'/g,"&#39;");
  }
  function short(u){ return esc(String(u||"").replace(/^https?:\/\//,"")); }
  // Cells are ellipsised at a fixed width, so every truncatable one carries the
  // full value as a tooltip; timestamps show the absolute time behind "3d ago".
  function at(t){
    if(!t || String(t).startsWith("0001")) return "";
    var d = new Date(t);
    return isNaN(d) ? "" : " title='"+attr(d.toLocaleString())+"'";
  }
  function epRow(e){
    var name = (e.prefix?esc(e.prefix)+" · ":"")+esc(e.project)+(e.solution?" <span class='sol'>("+esc(e.solution)+")</span>":"");
    var plain = (e.prefix?e.prefix+" · ":"")+(e.project||"")+(e.solution?" ("+e.solution+")":"");
    var url = e.state==="offline" ? short(e.url)
      : "<a href='"+attr(e.url)+"' target='_blank' rel='noopener'>"+short(e.url)+"</a>";
    return "<tr class='"+attr(e.state)+"'>"+
      "<td><span class='dot "+attr(e.state)+"'></span>"+esc(e.state)+"</td>"+
      "<td title='"+attr(plain)+"'>"+name+"</td>"+
      "<td title='"+attr(e.branch||"")+"'><code>"+esc(e.branch||"—")+"</code></td>"+
      "<td class='url' title='"+attr(e.url)+"'>"+url+"</td>"+
      "<td"+at(e.firstSeenAt)+">"+ago(e.firstSeenAt)+"</td>"+
      "<td"+at(e.lastSeenAt)+">"+ago(e.lastSeenAt)+"</td>"+
      "<td"+at(e.lastUsedAt)+">"+ago(e.lastUsedAt)+"</td>"+
      "<td>"+(e.uptimeSec?dur(e.uptimeSec):"—")+"</td></tr>";
  }
  function sessionCard(s){
    var eps = s.endpoints||[];
    var label = s.session ? esc(s.session) : "(no session)";
    var sid = s.sessionUrl ? "<a href='"+attr(s.sessionUrl)+"' target='_blank' rel='noopener'>"+label+"</a>" : label;
    var head = "<div class='sh'><span class='dot "+(s.online?"available":"offline")+"'></span>"+
      "<span class='sid'>"+sid+"</span>"+
      (s.owner?"<span class='own'>"+esc(s.owner)+"</span>":"")+
      "<span class='cnt'>"+eps.length+" endpoint"+(eps.length===1?"":"s")+"</span>"+
      "<span class='fs'"+at(s.firstSeen)+">since "+ago(s.firstSeen)+"</span>"+
      "<span class='ls'>seen "+ago(s.lastSeen)+"</span></div>";
    var table = "<table>` + epColgroup + `<thead><tr><th>Status</th><th>Project</th><th>Branch</th><th>URL</th>"+
      "<th>First seen</th><th>Last seen</th><th>Last used</th><th>Uptime</th></tr></thead><tbody>"+
      eps.map(epRow).join("")+"</tbody></table>";
    return "<section class='ses "+(s.online?"online":"offline")+"'>"+head+table+"</section>";
  }
  function render(data){
    document.getElementById("sessions").innerHTML = data.map(sessionCard).join("");
    document.getElementById("empty").hidden = data.length>0;
    var eps = data.reduce(function(n,s){ return n+((s.endpoints||[]).length); }, 0);
    document.getElementById("count").textContent =
      data.length+" session"+(data.length===1?"":"s")+" · "+eps+" endpoint"+(eps===1?"":"s");
    document.getElementById("updated").textContent = "updated "+new Date().toLocaleTimeString();
  }
  function load(){
    fetch("/api/sessions").then(function(r){return r.json();}).then(function(j){ render(j||[]); }).catch(function(){});
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
  .count { color:var(--mut); font-size:.88rem; margin-bottom:.7rem; }
  .danger { background:none; color:#ef4444; border:1px solid var(--line); margin-left:.6rem; }
  .danger:hover { border-color:#ef4444; }
  .keep { display:block; color:var(--mut); font-size:.85rem; margin-top:.8rem; }
</style></head>
<body>
<header><h1>mxcli tunnel-hub</h1><a href="/">← previews</a></header>
<div class="wrap">
  <div class="who" id="who">…</div>
  <p>Create a durable hub key to let <code class="inline">mxcli run --hub</code> register previews as you.
     The key does not expire and survives hub restarts — you set it once.</p>
  <div class="count" id="count"></div>
  <button id="mint">Create a hub key</button>
  <button id="revoke" class="danger" hidden>Revoke all keys</button>
  <label class="keep"><input type="checkbox" id="keep"> keep my existing keys (create an additional one)</label>
  <div id="out"></div>
</div>
<script>
(function(){
  function esc(s){ var d=document.createElement("div"); d.textContent=s==null?"":s; return d.innerHTML; }
  var signedIn=false;
  function refreshCount(){
    fetch("/api/keys").then(function(r){ return r.ok?r.json():null; }).then(function(j){
      var el=document.getElementById("count"), rev=document.getElementById("revoke");
      if(j){
        el.textContent = j.count===0 ? "You have no active keys." :
          "You have "+j.count+" active key"+(j.count===1?"":"s")+".";
        rev.hidden = j.count===0;
      }
    }).catch(function(){});
  }
  fetch("/api/whoami").then(function(r){return r.json();}).then(function(j){
    var who=document.getElementById("who");
    if(j && j.login){ who.innerHTML="Signed in as <b>"+esc(j.login)+"</b>."; signedIn=true; refreshCount(); }
    else { who.innerHTML="<span class='err'>Not signed in.</span> <a href='/auth/github/login?return="+encodeURIComponent(location.href)+"'>Sign in with GitHub</a>"; document.getElementById("mint").disabled=true; }
  });
  document.getElementById("mint").addEventListener("click", function(){
    var btn=this; btn.disabled=true; btn.textContent="Creating…";
    var keep=document.getElementById("keep").checked;
    fetch("/api/keys"+(keep?"?replace=false":""), {method:"POST", headers:{"X-Hub-Mint":"1"}}).then(function(r){
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
        "<p class='warn'>Copy it now — it is shown only once."+(keep?"":
        " Any previous keys were revoked (this replaced them).")+"</p>";
      document.getElementById("cp").addEventListener("click", function(){
        navigator.clipboard.writeText(j.key).then(function(){ this.textContent="Copied"; }.bind(this));
      });
      btn.textContent="Create a new key"; btn.disabled=false;
      refreshCount();
    }).catch(function(e){
      document.getElementById("out").innerHTML="<p class='err'>Could not create a key ("+esc(e.message)+"). Try signing in again.</p>";
      btn.textContent="Create a hub key"; btn.disabled=false;
    });
  });
  document.getElementById("revoke").addEventListener("click", function(){
    if(!confirm("Revoke ALL your hub keys? Any environment using MXCLI_HUB_KEY will stop registering until you set a new key.")) return;
    var btn=this; btn.disabled=true;
    fetch("/api/keys", {method:"DELETE", headers:{"X-Hub-Mint":"1"}}).then(function(r){ return r.json(); }).then(function(j){
      document.getElementById("out").innerHTML="<p>Revoked "+(j&&j.revoked||0)+" key(s). Create a new one when you need it.</p>";
      btn.disabled=false; refreshCount();
    }).catch(function(){ btn.disabled=false; });
  });
})();
</script>
</body></html>`
