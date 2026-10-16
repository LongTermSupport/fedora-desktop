"""Fixture of the rule project_hidden_bindings: its Python half (scripts/qa-hidden-bindings.bash
proves helpers/qa_environment/textual_bindings.py against this file on every run). A line
whose comment says finding must be reported and no other line may be. The gate never judges this tree
as code, and nothing runs it.
"""

from textual.app import App
from textual.binding import Binding
from textual.screen import Screen
from textual.widgets import Button

ACTION = "erase"


class Ask(Screen):
    BINDINGS = [("y", "confirm", "Yes"), ("p", "purge", "Purge")]  # finding

    def compose(self):
        yield Button("Yes (y)", action="screen.confirm")


class Manager(App):
    BINDINGS = [
        Binding("d", "download", "Download"),
        Binding("r", "remove", "Remove"),  # finding
        ("f5", "refresh", "Refresh"),  # finding
        Binding("x", ACTION),  # finding
        ("/", "focus_filter", "Filter"),
        ("q", "quit", "Quit"),
    ]

    def compose(self):
        yield Button("Download (d)", action="app.download")
        yield Button("Quit (q)", action="app.quit")
        yield Button("Purge (p)", action="app.purge")

    def on_mount(self):
        self.bind("e", "export")  # finding
