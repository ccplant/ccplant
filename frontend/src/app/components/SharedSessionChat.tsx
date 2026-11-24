"use client";

import SharedSessionChatView from "./SharedSessionChatView";
import {
  useSharedSessionData,
  useSharedSessionScroll,
} from "./useSharedSessionChat";

interface SharedSessionChatProps {
  shareToken: string;
}

export default function SharedSessionChat({
  shareToken,
}: SharedSessionChatProps) {
  const data = useSharedSessionData(shareToken);
  const scroll = useSharedSessionScroll(data.messages.length);
  return <SharedSessionChatView {...data} {...scroll} />;
}
