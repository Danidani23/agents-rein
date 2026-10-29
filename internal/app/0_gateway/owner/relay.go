package owner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// ErrNoOwner is the answer when no owner listens on the socket: the caller
// starts one.
var ErrNoOwner = errors.New("no background process answers")

// ErrOtherBuild is the answer of an owner that another build made. It has
// stopped taking commands and ends once its running ones have, so the
// caller starts one of its own build.
type ErrOtherBuild struct {
	Identity string
}

func (err ErrOtherBuild) Error() string {
	return "the background process is of another build, " + err.Identity
}

// Relay sends the command to the owner listening on socket and relays the
// caller's input and Ctrl-C to it, and the command's output and errors
// back, until the command ends; it returns the command's exit code.
func Relay(socket string, sent Request, in io.Reader, out io.Writer, errOut io.Writer, interrupts <-chan struct{}) (code int, err error) {
	body, err := json.Marshal(sent)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequest(http.MethodPost, "http://rein/v1/sessions", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", protocol)
	connection, reader, response, err := roundTrip(socket, request)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	switch response.StatusCode {
	case http.StatusSwitchingProtocols:
	case http.StatusConflict:
		var answer identityAnswer
		_ = json.NewDecoder(response.Body).Decode(&answer)
		return 0, ErrOtherBuild{Identity: answer.Identity}
	default:
		refusal, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return 0, fmt.Errorf("the background process did not take the command: %s", strings.TrimSpace(string(refusal)))
	}
	sending := &frames{connection: connection}
	done := make(chan struct{})
	defer close(done)
	go relayInput(sending, in)
	go func() {
		for {
			select {
			case <-done:
				return
			case _, open := <-interrupts:
				if !open {
					return
				}
				_ = sending.write(frameInterrupt, nil)
			}
		}
	}()
	for {
		kind, payload, err := readFrame(reader)
		if err != nil {
			return 0, fmt.Errorf("the background process ended before the command did: %w", err)
		}
		switch kind {
		case frameOutput:
			_, _ = out.Write(payload)
		case frameError:
			_, _ = errOut.Write(payload)
		case frameExit:
			return exitCodeOf(payload)
		}
	}
}

// relayInput sends what the caller types, then its end.
func relayInput(sending *frames, in io.Reader) {
	if in != nil {
		buffer := make([]byte, 32<<10)
		for {
			read, err := in.Read(buffer)
			if read > 0 && sending.write(frameInput, buffer[:read]) != nil {
				return
			}
			if err != nil {
				break
			}
		}
	}
	_ = sending.write(frameInputEnd, nil)
}

// Identify asks the owner on socket which build made it.
func Identify(socket string) (identity string, err error) {
	request, err := http.NewRequest(http.MethodGet, "http://rein/v1/identity", nil)
	if err != nil {
		return "", err
	}
	connection, _, response, err := roundTrip(socket, request)
	if err != nil {
		return "", err
	}
	defer connection.Close()
	var answer identityAnswer
	err = json.NewDecoder(response.Body).Decode(&answer)
	if err != nil {
		return "", fmt.Errorf("the background process answered who it is with something unreadable: %w", err)
	}
	return answer.Identity, nil
}

// roundTrip sends request over a connection of its own to the owner on
// socket and reads the answer's head; what follows it is in reader.
func roundTrip(socket string, request *http.Request) (connection net.Conn, reader *bufio.Reader, response *http.Response, err error) {
	connection, err = net.Dial("unix", socket)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %v", ErrNoOwner, err)
	}
	err = request.Write(connection)
	if err != nil {
		_ = connection.Close()
		return nil, nil, nil, fmt.Errorf("%w: %v", ErrNoOwner, err)
	}
	reader = bufio.NewReader(connection)
	response, err = http.ReadResponse(reader, request)
	if err != nil {
		_ = connection.Close()
		return nil, nil, nil, fmt.Errorf("%w: %v", ErrNoOwner, err)
	}
	return connection, reader, response, nil
}
