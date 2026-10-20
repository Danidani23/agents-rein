package declaration

import (
	"fmt"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
)

// Exception lets a team or a project skip a required declaration: it lives
// at the level that owns the declaration, exceptions/<id>.yaml, names the
// declaration and the grantee, says why, and may end. An expired exception
// is an error, and the declaration applies again.
type Exception struct {
	Declaration string    `yaml:"declaration"`
	Grantee     string    `yaml:"grantee"`
	Reason      string    `yaml:"reason"`
	Until       time.Time `yaml:"until,omitempty"`
	CreatedAt   time.Time `yaml:"created_at,omitempty"`
}

// ExceptionRecord is an exception with its own address and its owner's level.
type ExceptionRecord struct {
	Address string
	Owner   string
	Exception
}

// Expired says whether the exception ended before now.
func (exception Exception) Expired(now time.Time) bool {
	return !exception.Until.IsZero() && exception.Until.Before(now)
}

// ParseException reads one exception; file is its name without the
// extension, now decides whether it has expired.
func ParseException(file string, content []byte, now time.Time) (exception Exception, problems []Problem) {
	problems = decodeStrict(content, &exception, file+".yaml")
	if problems != nil {
		return Exception{}, problems
	}
	parsed, err := address.Parse(exception.Declaration)
	if err != nil || !parsed.IsDeclaration() {
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("declaration %q is not the address of a declaration", exception.Declaration)})
	}
	grantee, err := address.Parse(exception.Grantee)
	switch {
	case err != nil || grantee.IsDeclaration():
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("grantee %q is not the address of a team or a project", exception.Grantee)})
	case grantee.Kind() != address.Teams && grantee.Kind() != address.Projects:
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("grantee %q is not a team or a project", exception.Grantee)})
	}
	if strings.TrimSpace(exception.Reason) == "" {
		problems = append(problems, Problem{Severity: SeverityError, Message: "reason is required: why the grantee may skip the declaration"})
	}
	if exception.Expired(now) {
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("expired on %s; the declaration applies to %s again, so renew or remove the exception", exception.Until.Format("2006-01-02"), exception.Grantee)})
	}
	return exception, problems
}
