export function MessageAvatar({ role }: { role: string }) {
  return (
    <div
      className={`w-7 h-7 sm:w-8 sm:h-8 rounded-full flex items-center justify-center ${
        role === 'user' ? 'bg-blue-500 text-white' : 'bg-purple-500 text-white'
      }`}
    >
      {role === 'user' ? (
        <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="currentColor" viewBox="0 0 24 24">
          <path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" />
        </svg>
      ) : (
        <svg className="w-3 h-3 sm:w-4 sm:h-4" fill="currentColor" viewBox="0 0 24 24">
          <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z" />
        </svg>
      )}
    </div>
  );
}
