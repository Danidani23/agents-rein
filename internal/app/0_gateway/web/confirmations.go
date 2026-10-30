package web

import (
	"context"
	"html/template"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Confirmations are the questions that commands from a terminal or an
// agent wait on in the app's window (D53, R4): a reset of a project or of
// the machine, or a sync with --force. Each waits until the developer
// confirms or declines it on the window's page, or until its caller gives
// up. Only the window has the page: a browser's rein ui does not, since
// its token is printed where an agent can read it. Asked, when set, is
// told of each new question, so the app can show its window and a
// notification.
type Confirmations struct {
	Asked func(identifier string, asked ports.Confirmation)

	mutex   sync.Mutex
	waiting []*waitingConfirmation
	counter int
}

var _ ports.Confirmer = (*Confirmations)(nil)

// waitingConfirmation is one question on the page, with the channel its
// answer goes to.
type waitingConfirmation struct {
	Identifier string
	ports.Confirmation
	Since  time.Time
	answer chan bool
}

// Confirm waits for the developer's answer in the window.
func (confirmations *Confirmations) Confirm(ctx context.Context, asked ports.Confirmation) (confirmed bool, err error) {
	confirmations.mutex.Lock()
	confirmations.counter++
	waiting := &waitingConfirmation{Identifier: strconv.Itoa(confirmations.counter), Confirmation: asked, Since: time.Now(), answer: make(chan bool, 1)}
	confirmations.waiting = append(confirmations.waiting, waiting)
	confirmations.mutex.Unlock()
	if confirmations.Asked != nil {
		confirmations.Asked(waiting.Identifier, asked)
	}
	select {
	case confirmed = <-waiting.answer:
		return confirmed, nil
	case <-ctx.Done():
		confirmations.take(waiting.Identifier)
		return false, ctx.Err()
	}
}

// answer gives the question its answer; false when it no longer waits.
func (confirmations *Confirmations) answer(identifier string, confirmed bool) bool {
	waiting := confirmations.take(identifier)
	if waiting == nil {
		return false
	}
	waiting.answer <- confirmed
	return true
}

// take removes a question from the page and returns it, nil when it is
// not there.
func (confirmations *Confirmations) take(identifier string) *waitingConfirmation {
	confirmations.mutex.Lock()
	defer confirmations.mutex.Unlock()
	for index, waiting := range confirmations.waiting {
		if waiting.Identifier == identifier {
			confirmations.waiting = append(confirmations.waiting[:index], confirmations.waiting[index+1:]...)
			return waiting
		}
	}
	return nil
}

// list is every question waiting, oldest first.
func (confirmations *Confirmations) list() []waitingConfirmation {
	if confirmations == nil {
		return nil
	}
	confirmations.mutex.Lock()
	defer confirmations.mutex.Unlock()
	var listed []waitingConfirmation
	for _, waiting := range confirmations.waiting {
		listed = append(listed, *waiting)
	}
	return listed
}

// confirmationsView is the window's page of questions.
type confirmationsView struct {
	frame
	Questions []waitingConfirmation
}

// confirmationsPage shows every question waiting, each with Confirm and
// Decline.
func confirmationsPage(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		render(writer, page, confirmationsView{frame: frameOf(request, report, version, "Confirmations", "confirmations", actions), Questions: actions.confirmations.list()}, http.StatusOK)
	})
}

// answerConfirmation takes the developer's answer and leads back to the
// page.
func answerConfirmation(actions *runner) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		decision := request.FormValue("decision")
		if decision != "confirm" && decision != "decline" {
			http.Error(writer, "the answer is confirm or decline", http.StatusBadRequest)
			return
		}
		actions.confirmations.answer(request.FormValue("identifier"), decision == "confirm")
		http.Redirect(writer, request, "/confirmations", http.StatusSeeOther)
	})
}
