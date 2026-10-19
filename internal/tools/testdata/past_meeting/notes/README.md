<!-- Copyright The Linux Foundation and each contributor to LFX. -->
<!-- SPDX-License-Identifier: MIT -->

# get_past_meeting note and warning goldens

One pair per case: `<case>.golden` is the JSON array of the text blocks that
precede the JSON block (the "not visible" notes and the fetch-failure
warnings), `<case>.structured.json` whether the structured value carries a
`recording` and a `transcript`. The JSON block itself is not pinned here: its
shape changed by design (artifact file details without links), while the notes
and warnings are the contract these files pin.

Generated from the handler at main `97c867d0dc62e3a170d74708a22f8fa3773b724f`
(`git archive` of that commit plus a throwaway generator test over the same
stub answers), never from a branch that changes the handler. Regenerate the
same way only if a stub answer changes.
