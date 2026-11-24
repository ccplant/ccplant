import { useState } from "react";
import { type FontFamily, getFontSettings, setFontSettings as saveFontSettings } from "../../types/settings";
import { useFontSettingsListener } from "./useAgentChatUIEffects";

export function useAgentChatFontSettings() {
  const [fontSettings, setFontSettings] = useState(getFontSettings);
  useFontSettingsListener(setFontSettings);
  const handleFontSizeChange = (fontSize: number) => {
    saveFontSettings({ ...fontSettings, fontSize });
  };
  const handleFontFamilyChange = (fontFamily: FontFamily) => {
    saveFontSettings({ ...fontSettings, fontFamily });
  };
  return { fontSettings, setFontSettings, handleFontSizeChange, handleFontFamilyChange };
}
