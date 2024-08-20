import dynamic from "next/dynamic";

const Editor = dynamic(() => import("@/components/editor"), { ssr: false });

export default function Home() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-between p-24">
      <Editor />
    </main>
  );
}
