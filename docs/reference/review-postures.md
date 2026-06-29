# Review Postures

Review postures scope the findings a reviewer should emphasize. They do not
close the evidence set unless a prompt explicitly says so.

## compliance_license

`review_posture: compliance_license` asks the reviewer to focus findings on
license, attribution, telemetry, hosted-service, data-handling, regulatory, and
external-persistence risks.

It does not restrict evidence. A compliance-license reviewer must still inspect
the implementer handoff, the changed files named by that handoff, relevant
tests, and command outputs needed to verify the issue's acceptance criteria.
