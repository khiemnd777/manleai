export default function Loading() {
  return (
    <main className="min-h-screen bg-shell">
      <div className="mx-auto max-w-7xl px-5 py-5">
        <div className="h-12 rounded-md bg-white" />
        <div className="mt-5 h-[28rem] rounded-lg bg-white" />
        <div className="mt-6 grid gap-4 md:grid-cols-3">
          <div className="h-52 rounded-lg bg-white" />
          <div className="h-52 rounded-lg bg-white" />
          <div className="h-52 rounded-lg bg-white" />
        </div>
      </div>
    </main>
  );
}
