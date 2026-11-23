package demoseed

import (
	"math/rand/v2"
	"slices"
)

const (
	ReporterCount = 3
	BatchLimit    = 10
)

type Scenario struct {
	ID          string
	Title       string
	Description string
	System      string
}

// Compiled into the Go image. These are synthetic reports, not expected AI
// labels or staff decisions. IDs are stable even if the report text changes;
// never recycle an ID. Keep titles recognizable in mixed incident lists.
var scenarios = [...]Scenario{
	{"password-verification", "[Demo] Unexpected password verification email", "A message claiming to be from IT asked me to verify my password through a link. I did not click it.", "Corporate email"},
	{"email-link-login", "[Demo] Entered login after following an email link", "I followed a link in what looked like a bank email and entered my login. The next page looked wrong.", "Web browser"},
	{"renamed-files-ransom", "[Demo] Files renamed and ransom note appeared", "The documents in my team folder all end in .locked now, and there is a note asking for payment.", "Team file share"},
	{"mailbox-sent-messages", "[Demo] Messages sent from my mailbox", "Two colleagues received messages from my address this afternoon that I did not send.", "Corporate email"},
	{"wrong-spreadsheet-recipient", "[Demo] Spreadsheet went to the wrong recipient", "I sent a customer spreadsheet to an external address selected by autocomplete. I have asked the recipient not to open it.", "Corporate email"},
	{"laptop-on-train", "[Demo] Laptop left on the train", "I left my work laptop on the train this morning. Lost property has not found it yet.", "Company laptop"},
	{"unapproved-remote-support", "[Demo] Unapproved remote support software", "I found a remote support application on a shared workstation. Our IT team says it did not install it.", "Shared workstation"},
	{"staff-portal-outage", "[Demo] Staff portal unavailable", "My team cannot sign in to the staff portal. It started showing an unfamiliar error page this morning.", "Staff portal"},
	{"slow-laptop", "[Demo] Laptop running unusually slowly", "My laptop has been slow since this morning and its fan stays on, even when no applications are open.", "Company laptop"},
	{"supplier-payment-change", "[Demo] Supplier changed payment details", "A supplier emailed asking us to use a different bank account for the next invoice. We have not sent payment.", "Accounts payable"},
	{"gift-card-request", "[Demo] Suspicious gift card request", "Someone using our director's name asked me to buy gift cards urgently. The sender address did not look familiar.", "Corporate email"},
	{"browser-homepage-change", "[Demo] Browser homepage changed", "My browser homepage changed without me changing settings. I now see pop-up ads on pages I use for work.", "Web browser"},
	{"sk-courier-sms", "[Demo] Nezvyčajná SMS od kuriéra", "Prišla mi SMS s výzvou zaplatiť malý poplatok za doručenie cez odkaz. Odkaz som neotvoril.", "Služobný telefón"},
	{"sk-lost-phone", "[Demo] Stratený služobný telefón", "Po odchode z kancelárie som si všimla, že nemám služobný telefón. Na recepcii ho nenašli.", "Služobný telefón"},
	{"sk-unrecognized-login", "[Demo] Neznáme prihlásenie do účtu", "Dostal som upozornenie na prihlásenie do pracovného účtu v čase, keď som nebol pri počítači.", "Pracovný účet"},
}

func AvailableScenarios(used []string) []Scenario {
	available := make([]Scenario, 0, len(scenarios))
	for _, scenario := range scenarios {
		if !slices.Contains(used, scenario.ID) {
			available = append(available, scenario)
		}
	}
	return available
}

// SampleIncidents shuffles the caller's unused scenarios and selects up to BatchLimit.
func SampleIncidents(available []Scenario) []Scenario {
	rand.Shuffle(len(available), func(i, j int) { available[i], available[j] = available[j], available[i] })
	return available[:min(len(available), BatchLimit)]
}
