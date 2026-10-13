//go:build darwin

package clipfiles

import (
	"context"
	"os/exec"
)

// script JXA: URL-urile de fisier din NSPasteboard, cate o cale pe linie
const script = `ObjC.import("AppKit");
const pb = $.NSPasteboard.generalPasteboard;
const opts = $.NSDictionary.dictionaryWithObjectForKey(true, $.NSPasteboardURLReadingFileURLsOnlyKey);
const a = pb.readObjectsForClassesOptions($.NSArray.arrayWithObject($.NSURL), opts);
const out = [];
if (a) for (let i = 0; i < a.count; i++) out.push(ObjC.unwrap(a.objectAtIndex(i).path));
out.join("\n")`

// Paths: caile fisierelor din clipboard (nil daca nu sunt fisiere)
func Paths(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		return nil, err
	}
	return parseLines(string(out)), nil
}
