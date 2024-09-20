"use client";

import { useState } from "react";
import Monaco, { OnMount as OnEditorMount } from "@monaco-editor/react";
import { editor as Editor } from "monaco-editor";
import axios from "axios";
import { createRoot } from "react-dom/client";
import { Drum, LoaderCircle } from "lucide-react";

const rosePineDawnTheme: Editor.IStandaloneThemeData = {
  base: "vs",
  inherit: false,
  rules: [
    { token: "", foreground: "575279", background: "faf4ed" },
    { token: "keyword", foreground: "d7827e" },
    { token: "string", foreground: "ea9d34" },
    { token: "number", foreground: "907aa9" },
    { token: "comment", foreground: "9893a5", fontStyle: "italic" },
    { token: "type", foreground: "56949f" },
    { token: "function", foreground: "286983" },
    { token: "variable", foreground: "575279" },
    { token: "constant", foreground: "b4637a" },
    { token: "delimiter", foreground: "9893a5" },
    { token: "delimiter.bracket", foreground: "9893a5" },
    { token: "delimiter.parenthesis", foreground: "9893a5" },
    { token: "delimiter.square", foreground: "9893a5" },
    { token: "delimiter.angle", foreground: "9893a5" },
    { token: "delimiter.curly", foreground: "9893a5" },
    { token: "punctuation", foreground: "9893a5" },
    { token: "punctuation.bracket", foreground: "9893a5" },
    { token: "punctuation.parenthesis", foreground: "9893a5" },
  ],
  colors: {
    "editor.background": "#faf4ed",
    "editor.foreground": "#575279",
    "editorBracketMatch.border": "#9893a5",
    "editorBracketMatch.background": "#f4ede8",
    "editor.lineHighlightBackground": "#f4ede8",
    "editorLineNumber.foreground": "#9893a5",
    "editorCursor.foreground": "#575279",
    "editorWhitespace.foreground": "#dfdad9",
    "editor.selectionBackground": "#dfdad9",
    "editor.inactiveSelectionBackground": "#f4ede8",
    "scrollbarSlider.background": "#f2e9e1",
    "scrollbarSlider.hoverBackground": "#dfdad9",
    "scrollbarSlider.activeBackground": "#cecacd",
    "sideBar.background": "#fffaf3",
    "sideBar.foreground": "#797593",
    "sideBarTitle.foreground": "#9893a5",
    "sideBarSectionHeader.background": "#f2e9e1",
    "sideBarSectionHeader.foreground": "#575279",
    "editorBracketHighlight.foreground1": "#9893a5",
    "editorBracketHighlight.foreground2": "#9893a5",
    "editorBracketHighlight.foreground3": "#9893a5",
    "editorBracketHighlight.foreground4": "#9893a5",
    "editorBracketHighlight.foreground5": "#9893a5",
    "editorBracketHighlight.foreground6": "#9893a5",
  },
};

const options: Editor.IStandaloneEditorConstructionOptions = {
  minimap: { enabled: false },
  renderLineHighlight: "none",
  theme: "rose-pine-dawn",
  fontSize: 16,
  bracketPairColorization: { enabled: false },
  padding: { top: 16 },
  overviewRulerLanes: 0,
  hideCursorInOverviewRuler: true,
  overviewRulerBorder: false,
  scrollBeyondLastLine: false,
  automaticLayout: true,
  contextmenu: false,
};

interface ExecuteProps {
  editor: Editor.IStandaloneCodeEditor;
  monaco: typeof import("monaco-editor");
  execute: () => void;
}

const ExecuteButton = ({ editor, monaco, execute }: ExecuteProps) => {
  const [loading, setLoading] = useState(false);

  const click = async () => {
    if (loading) return;

    setLoading(true);
    await execute();
    setLoading(false);
  };

  editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, click);

  return (
    <button onClick={click} disabled={loading}>
      {loading ? (
        <LoaderCircle className="size-5 mt-5 animate-spin" />
      ) : (
        <Drum className="size-5 mt-5" />
      )}
    </button>
  );
};

export default function CodeEditor() {
  const [output, setOutput] = useState("");

  const setupEditor: OnEditorMount = (editor, monaco) => {
    async function execute() {
      setOutput("");
      const code = editor.getValue();
      await axios
        .post("/api/execute", { code })
        .then((resp) => {
          setOutput(resp.data.output);
        })
        .catch((error) => {
          if (error.status == 429) {
            setOutput(
              "You are sending too many requests. Please try again later.",
            );
          } else {
            setOutput(
              "An error occurred while executing the code. Please try again later.",
            );
          }
        });
    }

    const div = document.createElement("div");
    const root = createRoot(div);
    root.render(
      <ExecuteButton editor={editor} monaco={monaco} execute={execute} />,
    );

    const widget: Editor.IOverlayWidget = {
      getPosition: () => ({
        preference: Editor.OverlayWidgetPositionPreference.TOP_RIGHT_CORNER,
      }),
      getDomNode: () => div,
      getId: () => "toolbox",
    };
    editor.addOverlayWidget(widget);
  };

  return (
    <div className="w-full h-full">
      <div className="w-full h-full relative rounded overflow-hidden">
        <Monaco
          height="60vh"
          options={options}
          loading={null}
          value={`package main\n\nimport (\n    "fmt"\n)\n\nfunc main() {\n    fmt.Println("Hello, world!")\n}`}
          language="go"
          theme="rose-pine-dawn"
          beforeMount={(monaco) => {
            monaco.editor.defineTheme("rose-pine-dawn", rosePineDawnTheme);
            monaco.editor.setTheme("rose-pine-dawn");
          }}
          onMount={setupEditor}
        />
      </div>
      <div className="whitespace-pre-line">{output}</div>
    </div>
  );
}
