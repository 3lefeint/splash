/** Lets the "+" in the header of the pit setups tile ask the tile content for a new setup. */
class SetupsUi {
  requests = $state(0);
  /** The buttons of the tile can be moved and resized. */
  arrange = $state(false);

  create(): void {
    this.requests++;
  }
}

export const setupsUi = new SetupsUi();
