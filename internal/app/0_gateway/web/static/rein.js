// The one script of rein ui. On the page of a running action it follows
// the steps as the server sends them, one line per step, its outcome
// replacing its start as the CLI's line does, and reloads the page once
// the action has ended, which then shows the outcome. Everything else on
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
})();
