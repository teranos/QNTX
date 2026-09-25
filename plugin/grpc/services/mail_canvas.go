package services

// A mail from QNTX is drawn the way QNTX is: on its canvas, with windows
// standing on it (ADR-041).
//
// The colour of the canvas and of each window rides a table's bgcolor as well
// as its style. Some clients drop the styles of a body and keep a table's, and
// a background that is dropped leaves light text on white.

// canvasGrid is the canvas's 24px grid, as canvas.css draws it. A client that
// draws no background images shows the canvas's colour alone.
func canvasGrid() string {
	line := "transparent,transparent 23px," + Canvas.Grid + " 23px," + Canvas.Grid + " 24px"
	return "repeating-linear-gradient(0deg," + line + "),repeating-linear-gradient(90deg," + line + ")"
}

// CanvasPage is a whole mail on QNTX's canvas. inner is html, placed as it is.
func CanvasPage(inner string) string {
	return `<!DOCTYPE html>
<html>
<head><meta name="color-scheme" content="dark"><meta name="supported-color-schemes" content="dark"></head>
<body style="margin:0;padding:0;background-color:` + Canvas.Background + `;background-image:` + canvasGrid() + `">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="` + Canvas.Background + `" style="background-color:` + Canvas.Background + `;background-image:` + canvasGrid() + `">
<tr><td align="center" style="padding:24px 12px">
<table role="presentation" width="640" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:640px">
<tr><td style="font-family:` + Dark.Mono + `;font-size:13px;line-height:1.55;color:` + Dark.Text + `">
` + inner + `
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`
}

// CanvasWindow is one QNTX window standing on the canvas: a title bar with its
// symbol, and a body written in the ink of the element it resembles. title and
// body are html, placed as they are.
func CanvasWindow(symbol, title string, ink Ink, titleBar, body string) string {
	return `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="` + Canvas.Window + `" style="background-color:` + Canvas.Window + `;border:1px solid ` + Canvas.Border + `;border-radius:7px;border-collapse:separate;margin:0 0 16px">
<tr><td bgcolor="` + titleBar + `" style="background-color:` + titleBar + `;color:` + ink.Title + `;padding:7px 12px;font-size:12px;border-radius:7px 7px 0 0">` + symbol + `&nbsp;&nbsp;` + title + `</td></tr>
<tr><td style="padding:12px;color:` + ink.Value + `">` + body + `</td></tr>
</table>
`
}
