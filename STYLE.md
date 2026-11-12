# UI style

The UI uses the "Glam Terminal" look: pastel by day, neon by night, with a
sunset gradient, squishy candy buttons and little retro windows.

- **Tokens:** every colour, font, radius, shadow and duration comes from a
  `--gt-*` custom property in
  [`frontend/src/lib/styles/tokens.css`](frontend/src/lib/styles/tokens.css).
  Don't write raw colour values anywhere else; make tints with `color-mix()`.
- **Shared styles:** buttons (`.btn`, plus `.candy`, `.primary`, `.ghost`,
  `.danger`), fields (`.input`, `.field`), the window chrome (`.window`,
  `.titlebar`) and the pixel accent font (`.pixel`) are in
  [`base.css`](frontend/src/lib/styles/base.css). Everything else is in each
  component's scoped `<style>`.
- **Themes:** light ("Pastel Dream") and dark ("Midnight Mall"), switched by
  `data-theme` on `<html>`. The first run follows the system setting; the ☾/☀
  button in the bottom-left corner remembers a choice.
- **The window has no Windows frame.** The app's rounded border (`.shell`)
  is its edge, and the window behind it is transparent. The main panel's
  title bar holds minimise, maximise and close, and it and the sidebar
  header are the drag areas (`--wails-draggable`).

## Rules of thumb

- The sunset gradient is for title bars, the one primary button in a view,
  the volume bar, the idle sun and the hearts flair chip. Nowhere else.
- Text on a candy fill or on the gradient is always `--gt-ink`.
- Status is never colour alone: the badge carries the word.
- Hearts and flowers are decoration, marked `aria-hidden`, and stay out of
  error messages.
- All movement stops under `prefers-reduced-motion`.

## Where this departs from the look book

- **Smaller type and controls.** Body text is 15px and controls are 38px
  tall, a step below the book, because this is a small fixed-size window
  rather than a web page.
- **Three fonts, not five.** Fredoka (headings, buttons), Nunito (body) and
  VT323 (pixel accents), self-hosted via Fontsource. No script wordmark,
  since the window shows no logo, and stream URLs use the system monospace
  font.
- **No sparkle field, checkerboard strip or toggle sparkle burst.** The
  striped sun and grid horizon appear only when nothing is playing.
- **Wording is unchanged**, apart from the decorative title-bar label.
