# CyberGuard — Response Playbooks

Version: draft, 9 Nov 2026 · Author: Vitalina (domain expert) · Related: [Domain specification](domain-specification.md)

## Purpose

Short, approved step lists for each incident category. The LLM brief takes response steps **only** from these playbooks and does not invent its own. Analysts use them as a checklist, not as a script: they skip steps that do not apply.

Each playbook follows the NIST SP 800-61 order: understand the incident → contain the damage → remove the cause → recover → record and inform.

| Category | Playbook |
| --- | --- |
| `phishing` | [Phishing / social engineering](#phishing) |
| `malware` (incl. `ransomware`) | [Malicious software](#malware) |
| `account_compromise` | [Account compromise](#account_compromise) |
| `data_exposure` | [Data leak / exposure](#data_exposure) |
| `lost_device` | [Lost or stolen device](#lost_device) |
| `policy_violation` | [Security policy violation](#policy_violation) |
| `service_disruption` | [Service disruption](#service_disruption) |
| `other` | No playbook — the analyst decides case by case |

## phishing

1. Ask the employee whether they clicked the link, entered data, opened an attachment or made a payment.
2. If a password was entered — change it and end all active sessions of the account.
3. If a payment was made — contact the bank immediately to try to stop or recall it.
4. Check whether other employees received the same message.
5. Block the sender and the link's domain.
6. If an attachment was opened or a program installed — continue with the [malware](#malware) playbook.
7. Record indicators (sender address, domain, phone number) in an incident comment.
8. Tell the employee the outcome and remind them of the signs of phishing.

## malware

1. Disconnect the affected device from the network (cable and Wi-Fi), but do not switch it off.
2. Ask the employee what they opened, downloaded, plugged in or ran, and when.
3. **Ransomware** (encrypted files or a ransom note): set severity to `critical`, notify the admin immediately, check shared drives and backups. Do not pay and do not contact the attackers.
4. Check whether other devices or shared drives show the same signs.
5. Scan the device with up-to-date antivirus; if in doubt, reinstall it from a clean image.
6. Restore affected files from backup once the device is clean.
7. Change passwords that were used on the device.
8. Record the file name, source or sender in an incident comment.

## account_compromise

1. Reset the password and end all active sessions of the account.
2. Review the multi-factor authentication settings: remove unknown devices or methods and enrol again.
3. Review recent sign-ins: time, location and device.
4. Check mailbox rules and forwarding for changes the employee did not make, and remove them.
5. Find out what was sent or accessed from the account; warn recipients of any suspicious messages.
6. If the same password was used elsewhere, change it there too.
7. Record the findings and tell the employee how the account was most likely compromised.

## data_exposure

1. Find out what data was exposed, how much, and who received it or can access it.
2. Stop further exposure: revoke shared links, ask the recipient to delete the data, remove published content.
3. Determine whether personal data is involved.
4. If personal data is involved — inform the admin. Under GDPR, a personal data breach may have to be reported to the supervisory authority within 72 hours.
5. Where possible, get the recipient's confirmation that the data was deleted.
6. Record what was exposed and which measures were taken.
7. Remind the employee of safe sharing practices.

## lost_device

1. Find out the device type, when and where it was lost, whether it was locked or encrypted, and what data it held.
2. Lock or wipe the device remotely if it is managed by the company.
3. Change passwords for accounts used on the device and end their sessions.
4. If the device was stolen, recommend that the employee reports the theft to the police.
5. If it held unencrypted personal data — also follow the [data_exposure](#data_exposure) playbook.
6. Record the outcome and arrange a replacement device.

## policy_violation

1. Clarify which rule was broken, by whom and since when.
2. Stop the risky practice: remove unapproved software, change shared passwords, move data back to approved storage.
3. Check whether it caused damage; if so, continue with the matching playbook ([malware](#malware), [data_exposure](#data_exposure)).
4. Inform the employee's manager where needed.
5. Explain the rule and the approved alternative to the employee.
6. Record the violation and the measures taken.

## service_disruption

1. Confirm the scope: which system, who is affected and since when.
2. Contact the system owner or IT to tell an ordinary failure apart from an attack.
3. If there are no signs of an attack — close as `false_positive` and hand the issue over to IT.
4. If an attack is suspected (unusual traffic, strange messages) — escalate to the admin and work with the provider to block the traffic.
5. Inform affected users about the outage and the expected recovery time.
6. Restore the service and monitor it.
7. Record the cause and the measures taken.

## Sources

- NIST SP 800-61 — Computer Security Incident Handling Guide.
- CISA — Federal Government Cybersecurity Incident & Vulnerability Response Playbooks.
- SANS PICERL model (Preparation, Identification, Containment, Eradication, Recovery, Lessons learned).
