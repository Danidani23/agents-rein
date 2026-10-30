// The one script of rein ui. On the page of a running action it follows
// the steps as the server sends them, one line per step, its outcome
// replacing its start as the CLI's line does, and reloads the page once
// the action has ended, which then shows the outcome; where the stream
// fails, it reloads the page every two seconds instead. Everything else on
// the pages works without it.
(function () {
  "use strict";
  var run = document.querySelector('.run[data-running="true"]');
  if (!run || !window.EventSource) {
    return;
  }
  var steps = run.querySelector(".steps");

  function lineOf(key) {
    var lines = steps.children;
    for (var index = 0; index < lines.length; index++) {
      if (lines[index].dataset.key === key) {
        return lines[index];
      }
    }
    var line = document.createElement("li");
    line.dataset.key = key;
    var text = document.createElement("span");
    text.className = "text";
    var note = document.createElement("span");
    note.className = "note muted";
    line.appendChild(text);
    line.appendChild(note);
    steps.appendChild(line);
    return line;
  }

  var source = new EventSource("/run/events?from=" + encodeURIComponent(run.dataset.from));
  source.addEventListener("step", function (message) {
    var event = JSON.parse(message.data);
    var line = lineOf(event.key);
    if (event.kind === "detail") {
      var detail = document.createElement("div");
      detail.className = "detail muted";
      detail.textContent = event.detail;
      if (event.link) {
        var open = document.createElement("a");
        open.className = "button";
        open.href = event.link;
        open.target = "_blank";
        open.rel = "noopener";
        open.dataset.openLink = "";
        open.textContent = "Open";
        detail.appendChild(document.createTextNode(" "));
        detail.appendChild(open);
      }
      line.appendChild(detail);
      return;
    }
    if (event.kind === "note") {
      line.querySelector(".note").textContent = event.note;
      return;
    }
    line.className = event.kind;
    line.querySelector(".text").textContent = event.text;
  });
  source.addEventListener("end", function () {
    source.close();
    window.location.reload();
  });
  // where the stream cannot run, such as in a webview that does not carry it,
  // the page shows the steps so far each time it loads, so it loads again
  source.addEventListener("error", function () {
    source.close();
    window.setTimeout(function () {
      window.location.reload();
    }, 2000);
  });
})();

// in the app's window a link out opens in the browser, never in the window
document.addEventListener("click", function (event) {
  var link = event.target.closest ? event.target.closest("a[data-open-link]") : null;
  if (!link || document.body.dataset.window !== "true") {
    return;
  }
  event.preventDefault();
  fetch("/open-link", { method: "POST", body: new URLSearchParams({ link: link.href }) });
});
