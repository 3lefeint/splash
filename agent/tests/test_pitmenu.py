from splash_agent.pitmenu import PitMenuController, Row


class FakeMenu:
    """A pit menu that reacts to controls like the game; ``dead`` makes it ignore everything."""

    def __init__(self, dead=False):
        self.cats = [("FUEL:", [f"+{n}/{n // 20}" for n in range(123)]),
                     ("TIRES:", ["Soft", "Medium", "Hard", "No Change"]),
                     ("DRIVER:", ["Pascal", "Sasha", "Jules"])]
        self.cat, self.choice, self.dead, self.presses, self.holds, self.now = 0, 0, dead, [], [], 0.0
        self.choice = 20

    def read(self):
        name, choices = self.cats[self.cat]
        return Row(self.cat, name, self.choice, choices[self.choice], len(choices))

    def press(self, control, hold):
        self.presses.append(control)
        self.holds.append(hold)
        if self.dead:
            return
        if control == "PitMenuDown":
            self.cat, self.choice = (self.cat + 1) % len(self.cats), 0
        elif control == "PitMenuUp":
            self.cat, self.choice = (self.cat - 1) % len(self.cats), 0
        elif control == "PitMenuIncrementValue":
            self.choice = min(self.choice + 1, len(self.cats[self.cat][1]) - 1)
        elif control == "PitMenuDecrementValue":
            self.choice = max(self.choice - 1, 0)

    def sleep(self, s):
        self.now += s

    def clock(self):
        return self.now


def controller(menu):
    return PitMenuController(menu.read, menu.press, menu.sleep, menu.clock)


def test_max_fuel_and_a_named_tyre_are_set():
    m = FakeMenu()
    r = controller(m).apply([{"category": "FUEL:", "choice": "max"}, {"category": "TIRES:", "choice": "medium"}])
    assert r.ok and r.applied == [{"category": "FUEL:", "choice": "+122/6"}, {"category": "TIRES:", "choice": "Medium"}]


def test_a_number_of_litres_is_reached_from_both_sides():
    m = FakeMenu()
    assert controller(m).apply([{"category": "FUEL:", "choice": "35"}]).ok and m.choice == 35
    assert controller(m).apply([{"category": "fuel", "choice": "10"}]).ok and m.choice == 10


def test_min_goes_to_the_first_choice():
    m = FakeMenu()
    assert controller(m).apply([{"category": "FUEL:", "choice": "min"}]).ok and m.choice == 0


def test_unknown_row_or_choice_fails_with_a_message():
    r = controller(FakeMenu()).apply([{"category": "WEDGE:", "choice": "max"}])
    assert not r.ok and "WEDGE:" in r.message
    r = controller(FakeMenu()).apply([{"category": "TIRES:", "choice": "slick"}])
    assert not r.ok and "slick" in r.message


def test_a_menu_that_ignores_the_controls_fails_instead_of_looping():
    r = controller(FakeMenu(dead=True)).apply([{"category": "TIRES:", "choice": "soft"}])
    assert not r.ok and "does not react" in r.message


def test_no_menu_at_all():
    c = PitMenuController(lambda: None, lambda name, hold: None, lambda s: None, lambda: 0.0)
    assert not c.apply([{"category": "FUEL:", "choice": "max"}]).ok


def test_steps_done_before_a_failure_are_reported():
    r = controller(FakeMenu()).apply([{"category": "FUEL:", "choice": "50"}, {"category": "TIRES:", "choice": "x"}])
    assert not r.ok and r.applied == [{"category": "FUEL:", "choice": "+50/2"}]


def test_a_control_the_plugin_missed_is_sent_again_held_long():
    m = FakeMenu()
    real = m.press
    skipped = []

    def flaky(control, hold):
        if not skipped and hold < 0.1:  # the plugin only notices a quick control while it scans quickly
            skipped.append(control)
            m.presses.append(control)
            m.holds.append(hold)
            return
        real(control, hold)

    m.press = flaky
    assert controller(m).apply([{"category": "FUEL:", "choice": "25"}]).ok and m.choice == 25


def test_the_first_control_is_held_long_and_chained_ones_are_quick():
    m = FakeMenu()
    controller(m).apply([{"category": "FUEL:", "choice": "24"}])
    assert m.holds[0] > 0.1 and set(m.holds[1:]) == {0.04}
