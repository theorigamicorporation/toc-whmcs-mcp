package registry

// LongDescriptionThreshold is the length above which a vendor parameter
// description is treated as authored prose rather than a statement of fact.
//
// The distinction is a copyright one. "User ID", "Format: MMYY" and "The note
// to add" are facts about an interface, and where only a handful of phrasings
// exist for a fact there is no authorship to own. A hundred and forty
// characters explaining how to base64-encode a serialized PHP array is
// somebody's writing.
//
// This project is AGPL-3.0, which means asserting that everything in it can be
// redistributed under that licence. That assertion cannot be made about text
// WHMCS wrote. So cmd/docgen refuses to emit any vendor description longer than
// this threshold: it uses the replacement below if one exists, and emits
// nothing if one does not.
//
// The threshold is a crude proxy for expressive room, and deliberately so: it
// has to be mechanical, because regeneration must stay deterministic for
// `just gen-check` to mean anything. Erring low costs a description; erring
// high costs a licence claim we cannot support.
const LongDescriptionThreshold = 100

// descriptions replaces vendor prose with our own, keyed by "action.parameter"
// in lower case.
//
// These are written for an agent rather than for a developer reading a manual:
// state the encoding, the format and the constraint plainly, and drop the
// worked PHP examples, which an agent cannot execute anyway.
//
// Adding an entry is how a parameter gets its explanation back. Leaving one out
// is safe: the parameter still carries its name, type and requiredness, and
// whmcs_describe_action still links to the vendor's own page, which is
// authoritative.
//
// #nosec G101 -- every value here is documentation prose. gosec's
// hardcoded-credential heuristic matches on words like "card_number",
// "password" and "token", which is exactly what descriptions of those
// parameters have to contain. There is no secret in this file, and the
// server never returns a credential in the first place.
var descriptions = map[string]string{
	// ---- Client ----------------------------------------------------------
	"addclient.owner_user_id":  "Existing user to own the client. Omit to create a new user.",
	"addclient.firstname":      "Client first name. Also becomes the user's first name when owner_user_id is omitted.",
	"addclient.lastname":       "Client last name. Also becomes the user's last name when owner_user_id is omitted.",
	"addclient.email":          "Client email address. Also becomes the user's email when owner_user_id is omitted.",
	"addclient.securityqid":    "Security question ID from the admin security questions table. Required when owner_user_id is omitted.",
	"addclient.language":       "Default language, given as a full name such as 'english' or 'french'. Also sets the user's language when owner_user_id is omitted.",
	"addclient.skipvalidation": "Set true to skip required-field enforcement. Email and password are still enforced when owner_user_id is omitted.",
	"addclientnote.sticky":     "Pin the note so it appears throughout the client's account and on tickets they submit.",

	// ---- Billing ---------------------------------------------------------
	"addpaymethod.type":                "Pay method type: BankAccount, CreditCard or RemoteCreditCard. Defaults to CreditCard.",
	"createquote.phonenumber":          "Client phone number in local format, without a country code, used when userid is omitted.",
	"updateinvoice.itemdescription":    "Map of existing line item ID to replacement description. Line item IDs come from GetInvoice.",
	"updateinvoice.newitemdescription": "Descriptions for new line items, as a list.",
	"updateinvoice.newitemtaxed":       "Taxable flag for each new line item, as a list positionally matching newitemdescription.",
	"updateinvoice.deletelineids":      "Line item IDs to remove. IDs come from GetInvoice.",
	// Card fields on UpdatePayMethod are refused by this server regardless;
	// the descriptions exist so the refusal is understandable.
	"updatepaymethod.card_number":       "Card number. Applies to CreditCard and RemoteCreditCard types. This server refuses card parameters.",
	"updatepaymethod.card_expiry":       "Card expiry as MMYY, for example 0122. This server refuses card parameters.",
	"updatepaymethod.card_start":        "Card start date as MMYY, for example 0122. This server refuses card parameters.",
	"updatepaymethod.card_issue_number": "Card issue number. This server refuses card parameters.",
	"updatepaymethod.bank_account_type": "Bank account type, such as checking or credit. Send only to change it.",
	"updatepaymethod.bank_code":         "Bank code, also called a sort code or routing number. Send only to change it.",
	"updatepaymethod.bank_account":      "Bank account number. Required for the BankAccount type. Send only to change it.",

	// ---- Orders and products ---------------------------------------------
	"acceptorder.autosetup":                        "Run the product module to provision the service. Overrides the product's own setup setting.",
	"fraudorder.cancelsub":                         "Also cancel any PayPal subscriptions attached to the order's products and services.",
	"addproduct.subdomain":                         "Comma-separated subdomains offered on the order form, for example .example.com,.example.net",
	"addproduct.autosetup":                         "When to provision automatically: empty for never, 'on' for pending order, 'payment' on payment, 'order' on order.",
	"addproduct.recommendations":                   "Product recommendations, each with a product id and an integer sort order.",
	"addproduct.ondemandrenewalsenabled":           "Enable on-demand renewals. Requires ondemandrenewalconfigurationoverride to be true.",
	"addproduct.ondemandrenewalperiodmonthly":      "Days before due date that early renewal is allowed on monthly billing. Requires ondemandrenewalconfigurationoverride.",
	"addproduct.ondemandrenewalperiodquarterly":    "Days before due date that early renewal is allowed on quarterly billing. Requires ondemandrenewalconfigurationoverride.",
	"addproduct.ondemandrenewalperiodsemiannually": "Days before due date that early renewal is allowed on semi-annual billing. Requires ondemandrenewalconfigurationoverride.",
	"addproduct.ondemandrenewalperiodannually":     "Days before due date that early renewal is allowed on annual billing. Requires ondemandrenewalconfigurationoverride.",
	"addproduct.ondemandrenewalperiodbiennially":   "Days before due date that early renewal is allowed on two-year billing. Requires ondemandrenewalconfigurationoverride.",
	"addproduct.ondemandrenewalperiodtriennially":  "Days before due date that early renewal is allowed on three-year billing. Requires ondemandrenewalconfigurationoverride.",
	"updateclientproduct.unset":                    "Fields to clear. One or more of: domain, serviceusername, servicepassword, subscriptionid, ns1, ns2, dedicatedip, assignedips, notes, suspendreason.",
	"updateclientproduct.autorecalc":               "Recalculate the recurring amount automatically, ignoring any recurringamount supplied.",
	"updateclientproduct.customfields":             "Custom field values as base64 of a PHP-serialized array mapping custom field ID to value.",
	"updateclientproduct.configoptions":            "Configurable options as base64 of a PHP-serialized array mapping config option ID to the chosen option ID, or to an array of optionid and qty for quantity-based options.",
	"upgradeproduct.configoptions":                 "Config options for the upgrade when type is configoptions. Maps config option ID to the chosen option ID, or to a value for value-based options.",
	"updateclientaddon.autorecalc":                 "Recalculate the recurring amount automatically, ignoring any recurring value supplied.",

	// ---- Domains ---------------------------------------------------------
	"createorupdatetld.currency_code": "Currency the supplied pricing is expressed in. Required when setting pricing, grace fee or redemption fee. Prices are converted into every active currency, and the currency must already exist in the target WHMCS install.",
	"createorupdatetld.renew":         "Renewal pricing by period. Nine years is the longest renewal period accepted.",
	"createorupdatetld.transfer":      "Transfer pricing. Only the minimum registration period can be priced.",
	"domainregister.idnlanguage":      "IDN language code, overriding whatever is stored on the domain.",
	"domainrequestepp.eppcode":        "EPP transfer code, when the registrar returns one. No code and no error means the registrar sent it to the client directly. A returned code may contain HTML entities and must be decoded before use.",
	"gettldpricing.currencyid":        "Currency to price in. Supply this or clientid, not both; clientid wins if both are given.",
	"gettldpricing.clientid":          "Client whose currency to price in. Supply this or currencyid, not both; clientid wins if both are given.",
	"gettldpricing.pricing":           "Pricing per TLD. Entries appear only for TLDs that have pricing configured, including a configured price of zero.",
	"updateclientdomain.autorecalc":   "Recalculate the recurring amount automatically, ignoring any recurringamount supplied.",

	// ---- Support ---------------------------------------------------------
	"addticketnote.attachments":         "File attachments as base64 of a JSON array. Each entry needs a filename and its data.",
	"addticketreply.attachments":        "File attachments as base64 of a JSON array. Each entry needs a filename and its data.",
	"addticketreply.status":             "Ticket status to set after replying, when the department's default response status is not wanted. Valid values come from GetSupportStatuses.",
	"openticket.attachments":            "File attachments as base64 of a JSON array. Each entry needs a filename and its data.",
	"openticket.admin":                  "Open the ticket as an admin. The admin username must also be supplied, otherwise the client is recorded as the opener.",
	"openticket.preventclientclosure":   "Prevent the client closing the ticket. Inherits the department setting when omitted.",
	"updateticket.preventclientclosure": "Prevent the client closing the ticket. When omitted and the ticket moves department, it inherits the new department's setting.",
	"gettickets.status":                 "Filter by ticket status. Any configured status, plus Awaiting Reply, All Active Tickets and My Flagged Tickets.",

	// ---- System and modules ----------------------------------------------
	"activatemodule.parameters":            "Module configuration values to set. Use GetModuleConfigurationParameters to discover the fields a module accepts.",
	"updatemoduleconfiguration.parameters": "Module configuration values to set. Use GetModuleConfigurationParameters to discover the fields a module accepts.",
	"getemailtemplates.language":           "Language of the templates to return. Defaults to the default-language templates.",
	"sendemail.id":                         "Record the template relates to, for example a client ID for a general template.",
	"sendemail.customtype":                 "Custom template type: general, product, domain, invoice, support or affiliate.",
	"triggernotificationevent.statusstyle": "Status styling for the notification: success, danger or info.",
	"triggernotificationevent.attributes":  "Attributes to attach to the notification. Each needs at least a label and a value.",

	// ---- Authentication (blocked actions, kept for describe output) -------
	"createssotoken.user_id":                    "User to authenticate. Defaults to the owner of the requested client.",
	"createoauthcredential.logouri":             "Logo image for the application, as a URL or a path relative to the WHMCS client area root.",
	"updateoauthcredential.clientapiidentifier": "OAuth client ID to update. Needed only when credentialId is not supplied.",
	"updateoauthcredential.granttype":           "Grant type the credential is valid for: authorization_code or single_sign_on.",
	"updateoauthcredential.scope":               "Space-delimited scopes the credential is valid for. CreateOAuthCredential documents the permitted values.",
}

// Description returns our replacement for a vendor description, if one exists.
// cmd/docgen calls it during generation; the key is case-insensitive.
func Description(action, param string) (string, bool) {
	d, ok := descriptions[canonicalName(action)+"."+canonicalName(param)]
	return d, ok
}

// DescriptionCount reports how many replacements are defined, for tests.
func DescriptionCount() int { return len(descriptions) }
