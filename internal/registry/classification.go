package registry

// classification is the hand-maintained safety class for every WHMCS action.
//
// The vendor documentation does not distinguish reading from destroying, so
// this table is human judgement and is the most security-sensitive file in the
// repository. Two rules govern edits:
//
//  1. When in doubt, classify higher. An action classified destructive that is
//     merely a write costs an operator one confirmation step. An action
//     classified read that actually terminates hosting costs a customer their
//     server.
//  2. An action absent from this table is treated as ClassWrite at runtime, and
//     cmd/docgen fails outright, so a WHMCS upgrade cannot quietly introduce an
//     unclassified action.
//
// "Destructive" here is broader than "deletes a row". It also covers anything
// that moves money, changes provisioning, alters global configuration, or sends
// mail to a customer, because none of those can be undone by the operator who
// triggered them.
var classification = map[string]Class{
	// ---- Orders ----------------------------------------------------------
	"AcceptOrder":      ClassDestructive, // provisions and invoices
	"AddOrder":         ClassWrite,
	"CancelOrder":      ClassDestructive,
	"DeleteOrder":      ClassDestructive,
	"FraudOrder":       ClassWrite,
	"GetOrders":        ClassRead,
	"GetOrderStatuses": ClassRead,
	"GetProducts":      ClassRead,
	"GetPromotions":    ClassRead,
	"OrderFraudCheck":  ClassWrite,
	"PendingOrder":     ClassWrite,

	// ---- Billing ---------------------------------------------------------
	"AcceptQuote":       ClassDestructive, // converts to an order and invoices
	"AddBillableItem":   ClassWrite,
	"AddCredit":         ClassDestructive, // moves money
	"AddInvoicePayment": ClassDestructive, // records a payment; not reversible
	"AddPayMethod":      ClassWrite,
	"AddTransaction":    ClassDestructive, // moves money
	"ApplyCredit":       ClassDestructive, // moves money
	"CapturePayment":    ClassDestructive, // charges the customer
	"CreateInvoice":     ClassWrite,
	"CreateQuote":       ClassWrite,
	"DeletePayMethod":   ClassDestructive,
	"DeleteQuote":       ClassDestructive,
	"GenInvoices":       ClassDestructive, // bulk invoice generation and mail
	"GetCredits":        ClassRead,
	"GetInvoice":        ClassRead,
	"GetInvoices":       ClassRead,
	"GetPayMethods":     ClassRead,
	"GetQuotes":         ClassRead,
	"GetTransactions":   ClassRead,
	"SendQuote":         ClassDestructive, // mails the customer
	"UpdateInvoice":     ClassWrite,
	"UpdatePayMethod":   ClassWrite,
	"UpdateQuote":       ClassWrite,
	"UpdateTransaction": ClassWrite,

	// ---- Module ----------------------------------------------------------
	// Module state changes affect every service using the module.
	"ActivateModule":                   ClassDestructive,
	"DeactivateModule":                 ClassDestructive,
	"GetModuleConfigurationParameters": ClassRead,
	"GetModuleQueue":                   ClassRead,
	"UpdateModuleConfiguration":        ClassDestructive,

	// ---- Support ---------------------------------------------------------
	"AddAnnouncement":    ClassWrite,
	"AddCancelRequest":   ClassWrite,
	"AddClientNote":      ClassWrite,
	"AddTicketNote":      ClassWrite,       // internal note, not sent to the customer
	"AddTicketReply":     ClassDestructive, // mails the customer
	"BlockTicketSender":  ClassWrite,
	"DeleteAnnouncement": ClassDestructive,
	"DeleteTicket":       ClassDestructive,
	"DeleteTicketNote":   ClassDestructive,
	"DeleteTicketReply":  ClassDestructive,
	"GetAnnouncements":   ClassRead,
	"MergeTicket":        ClassDestructive, // not cleanly reversible
	"OpenTicket":         ClassWrite,
	"UpdateTicket":       ClassWrite,
	"UpdateTicketReply":  ClassWrite,

	// ---- System ----------------------------------------------------------
	"AddBannedIp": ClassWrite,
	// Password oracles. There is no legitimate reason for an LLM agent to hold
	// a decrypted credential, and no configuration enables these.
	"DecryptPassword":          ClassBlocked,
	"EncryptPassword":          ClassBlocked,
	"GetActivityLog":           ClassRead,
	"GetAdminDetails":          ClassRead,
	"GetAdminUsers":            ClassRead,
	"GetAutomationLog":         ClassRead,
	"GetConfigurationValue":    ClassRead,
	"GetCurrencies":            ClassRead,
	"GetEmailTemplates":        ClassRead,
	"GetPaymentMethods":        ClassRead,
	"GetStaffOnline":           ClassRead,
	"GetStats":                 ClassRead,
	"GetToDoItems":             ClassRead,
	"GetToDoItemStatuses":      ClassRead,
	"LogActivity":              ClassWrite,
	"SendAdminEmail":           ClassWrite,       // internal recipients only
	"SendEmail":                ClassDestructive, // mails the customer
	"SetConfigurationValue":    ClassDestructive, // global system setting
	"TriggerNotificationEvent": ClassWrite,
	"UpdateAdminNotes":         ClassWrite,
	"UpdateAnnouncement":       ClassWrite,
	"UpdateToDoItem":           ClassWrite,
	"WhmcsDetails":             ClassRead,

	// ---- Client ----------------------------------------------------------
	"AddClient":            ClassWrite,
	"AddContact":           ClassWrite,
	"CloseClient":          ClassDestructive,
	"DeleteClient":         ClassDestructive,
	"DeleteContact":        ClassDestructive,
	"GetCancelledPackages": ClassRead,
	"GetClientGroups":      ClassRead,
	"GetClientPassword":    ClassBlocked, // returns a credential
	"GetClients":           ClassRead,
	"GetClientsAddons":     ClassRead,
	"GetClientsDetails":    ClassRead,
	"GetClientsDomains":    ClassRead,
	"GetClientsProducts":   ClassRead,
	"GetContacts":          ClassRead,
	"GetEmails":            ClassRead,
	"UpdateClient":         ClassWrite,
	"UpdateContact":        ClassWrite,

	// ---- Products --------------------------------------------------------
	"AddProduct": ClassWrite,

	// ---- Project Management ---------------------------------------------
	"AddProjectMessage": ClassWrite,
	"AddProjectTask":    ClassWrite,
	"CreateProject":     ClassWrite,
	"DeleteProjectTask": ClassDestructive,
	"EndTaskTimer":      ClassWrite,
	"GetProject":        ClassRead,
	"GetProjects":       ClassRead,
	"StartTaskTimer":    ClassWrite,
	"UpdateProject":     ClassWrite,
	"UpdateProjectTask": ClassWrite,

	// ---- Users -----------------------------------------------------------
	"AddUser":               ClassWrite,
	"CreateClientInvite":    ClassDestructive, // mails an invitation
	"DeleteUserClient":      ClassDestructive, // revokes account access
	"GetPermissionsList":    ClassRead,
	"GetUserPermissions":    ClassRead,
	"GetUsers":              ClassRead,
	"ResetPassword":         ClassDestructive, // locks the customer out
	"UpdateUser":            ClassWrite,
	"UpdateUserPermissions": ClassDestructive, // privilege change

	// ---- Affiliates ------------------------------------------------------
	"AffiliateActivate": ClassWrite,
	"GetAffiliates":     ClassRead,

	// ---- Authentication --------------------------------------------------
	// Credential and session issuance. An agent that can mint an SSO token or
	// an OAuth credential has escaped every other control in this server.
	"CreateOAuthCredential": ClassBlocked,
	"CreateSsoToken":        ClassBlocked,
	"DeleteOAuthCredential": ClassDestructive,
	"ListOAuthCredentials":  ClassRead,
	"UpdateOAuthCredential": ClassBlocked,
	"ValidateLogin":         ClassBlocked, // password-checking oracle

	// ---- Domains ---------------------------------------------------------
	"CreateOrUpdateTLD":         ClassDestructive, // affects pricing globally
	"DomainGetLockingStatus":    ClassRead,
	"DomainGetNameservers":      ClassRead,
	"DomainGetWhoisInfo":        ClassRead,
	"DomainRegister":            ClassDestructive, // spends money at the registrar
	"DomainRelease":             ClassDestructive,
	"DomainRenew":               ClassDestructive, // spends money at the registrar
	"DomainRequestEPP":          ClassDestructive, // enables transfer away
	"DomainToggleIdProtect":     ClassWrite,
	"DomainTransfer":            ClassDestructive, // spends money at the registrar
	"DomainUpdateLockingStatus": ClassWrite,
	"DomainUpdateNameservers":   ClassWrite,
	"DomainUpdateWhoisInfo":     ClassWrite,
	"DomainWhois":               ClassRead,
	"GetRegistrars":             ClassRead,
	"GetTLDPricing":             ClassRead,
	"UpdateClientDomain":        ClassWrite,

	// ---- Servers ---------------------------------------------------------
	"GetHealthStatus": ClassRead,
	"GetServers":      ClassRead, // response carries server credentials; redacted

	// ---- Tickets ---------------------------------------------------------
	"GetSupportDepartments":      ClassRead,
	"GetSupportStatuses":         ClassRead,
	"GetTicket":                  ClassRead,
	"GetTicketAttachment":        ClassRead,
	"GetTicketCounts":            ClassRead,
	"GetTicketNotes":             ClassRead,
	"GetTicketPredefinedCats":    ClassRead,
	"GetTicketPredefinedReplies": ClassRead,
	"GetTickets":                 ClassRead,

	// ---- Service ---------------------------------------------------------
	// Every module call reaches into the provisioning system and affects a
	// running customer service.
	"ModuleChangePackage": ClassDestructive,
	"ModuleChangePw":      ClassDestructive,
	"ModuleCreate":        ClassDestructive,
	"ModuleCustom":        ClassDestructive, // arbitrary module function
	"ModuleSuspend":       ClassDestructive,
	"ModuleTerminate":     ClassDestructive,
	"ModuleUnsuspend":     ClassWrite,
	"UpdateClientProduct": ClassWrite,
	"UpgradeProduct":      ClassDestructive, // creates a billable upgrade

	// ---- Addons ----------------------------------------------------------
	"UpdateClientAddon": ClassWrite,
}

// classByCanonical indexes the table case-insensitively, matching how WHMCS
// itself treats action names and how Lookup resolves them. The table above is
// written in the vendor's casing because that is how a reviewer reads it.
var classByCanonical = func() map[string]Class {
	m := make(map[string]Class, len(classification))
	for name, c := range classification {
		m[canonicalName(name)] = c
	}
	return m
}()

// Classify returns the safety class of an action. An action that is not in the
// table is reported as ClassWrite, never ClassRead, so an unclassified action
// can never be treated as safe. Generation fails on unclassified actions, so
// this fallback should be unreachable in a built binary.
func Classify(action string) Class {
	if c, ok := classByCanonical[canonicalName(action)]; ok {
		return c
	}
	return ClassWrite
}

// Classified reports whether an action has an explicit classification. cmd/docgen
// uses this to fail generation when the vendor adds an action.
func Classified(action string) bool {
	_, ok := classByCanonical[canonicalName(action)]
	return ok
}

// ClassifiedNames returns every action name in the classification table.
func ClassifiedNames() []string {
	names := make([]string, 0, len(classification))
	for n := range classification {
		names = append(names, n)
	}
	return names
}
