/* =============================================================================
   Alpine entry — the policy-safe (CSP) build.

   Directives carry component and property names, never expressions, so nothing
   is evaluated at runtime and script-src stays 'self'. The default build would
   need 'unsafe-eval', which this application does not grant.

   Every component here owns one element's state and nothing else: what is
   open, chosen, revealed, busy. Anything that listens on the document, speaks
   to the network, or holds a media recorder stays a plain module — see
   web/src/modules and STACK.md §2.3.
   ========================================================================== */

import Alpine from "@alpinejs/csp";

import matchFormat from "../components/matchFormat.js";
import ticketKind from "../components/ticketKind.js";
import ticketPane from "../components/ticketPane.js";
import sourcePick from "../components/sourcePick.js";
import sourcePane from "../components/sourcePane.js";
import cannedReply from "../components/cannedReply.js";
import passwordReveal from "../components/passwordReveal.js";
import colourField from "../components/colourField.js";
import questionNote from "../components/questionNote.js";
import questionRating from "../components/questionRating.js";
import ratingStar from "../components/ratingStar.js";
import authoredQuestions from "../components/authoredQuestions.js";

// One registration per component, by name. The markup summons a component with
// x-data="name", so this list is the whole vocabulary a template may use.
Alpine.data("matchFormat", matchFormat);
Alpine.data("ticketKind", ticketKind);
Alpine.data("ticketPane", ticketPane);
Alpine.data("sourcePick", sourcePick);
Alpine.data("sourcePane", sourcePane);
Alpine.data("cannedReply", cannedReply);
Alpine.data("passwordReveal", passwordReveal);
Alpine.data("colourField", colourField);
Alpine.data("questionNote", questionNote);
Alpine.data("questionRating", questionRating);
Alpine.data("ratingStar", ratingStar);
Alpine.data("authoredQuestions", authoredQuestions);

window.Alpine = Alpine;
Alpine.start();
