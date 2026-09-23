/**
 * Label wrapping, shared by every section.
 *
 * A finding can be one long sentence carrying its diagnostic and the command that clears
 * it, and a play row carries a repo path. Unwrapped, a menu label is as wide as its text,
 * so the popup ran off the screen as a single line. The width cap is the stylesheet's
 * `max-width`.
 */

import Pango from 'gi://Pango';

export function wrap(label) {
    label.clutter_text.line_wrap = true;
    label.clutter_text.line_wrap_mode = Pango.WrapMode.WORD_CHAR;
    label.clutter_text.ellipsize = Pango.EllipsizeMode.NONE;
}
