package demoseed

import "math/rand/v2"

const (
	ReporterCount = 3
	IncidentCount = 10
)

type Scenario struct {
	Title       string
	Description string
	System      string
}

// Compiled into the Go image. These are synthetic reports, not expected AI
// labels or staff decisions. Keep titles recognizable in mixed incident lists.
var scenarios = [...]Scenario{
	{"[Demo] Unexpected password verification email", "A message claiming to be from IT asked me to verify my password through a link. I did not click it.", "Corporate email"},
	{"[Demo] Entered login after following an email link", "I followed a link in what looked like a bank email and entered my login. The next page looked wrong.", "Web browser"},
	{"[Demo] Files renamed and ransom note appeared", "The documents in my team folder all end in .locked now, and there is a note asking for payment.", "Team file share"},
	{"[Demo] Messages sent from my mailbox", "Two colleagues received messages from my address this afternoon that I did not send.", "Corporate email"},
	{"[Demo] Spreadsheet went to the wrong recipient", "I sent a customer spreadsheet to an external address selected by autocomplete. I have asked the recipient not to open it.", "Corporate email"},
	{"[Demo] Laptop left on the train", "I left my work laptop on the train this morning. Lost property has not found it yet.", "Company laptop"},
	{"[Demo] Unapproved remote support software", "I found a remote support application on a shared workstation. Our IT team says it did not install it.", "Shared workstation"},
	{"[Demo] Staff portal unavailable", "My team cannot sign in to the staff portal. It started showing an unfamiliar error page this morning.", "Staff portal"},
	{"[Demo] Laptop running unusually slowly", "My laptop has been slow since this morning and its fan stays on, even when no applications are open.", "Company laptop"},
	{"[Demo] Supplier changed payment details", "A supplier emailed asking us to use a different bank account for the next invoice. We have not sent payment.", "Accounts payable"},
	{"[Demo] Suspicious gift card request", "Someone using our director's name asked me to buy gift cards urgently. The sender address did not look familiar.", "Corporate email"},
	{"[Demo] Browser homepage changed", "My browser homepage changed without me changing settings. I now see pop-up ads on pages I use for work.", "Web browser"},
	{"[Demo] Nezvyčajná SMS od kuriéra", "Prišla mi SMS s výzvou zaplatiť malý poplatok za doručenie cez odkaz. Odkaz som neotvoril.", "Služobný telefón"},
	{"[Demo] Stratený služobný telefón", "Po odchode z kancelárie som si všimla, že nemám služobný telefón. Na recepcii ho nenašli.", "Služobný telefón"},
	{"[Demo] Neznáme prihlásenie do účtu", "Dostal som upozornenie na prihlásenie do pracovného účtu v čase, keď som nebol pri počítači.", "Pracovný účet"},
}

// SampleIncidents selects distinct scenarios in random order.
func SampleIncidents() []Scenario {
	indices := rand.Perm(len(scenarios))[:IncidentCount]
	sample := make([]Scenario, 0, IncidentCount)
	for _, index := range indices {
		sample = append(sample, scenarios[index])
	}
	return sample
}
