import type { GlobalThemeOverrides } from "naive-ui";

const systemFont =
  '"Geist Variable", -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif';

const sharedOverrides = {
  common: {
    primaryColor: "#171717",
    primaryColorHover: "#383838",
    primaryColorPressed: "#4d4d4d",
    primaryColorSuppl: "#171717",
    infoColor: "#0070f3",
    successColor: "#29a383",
    warningColor: "#f5a623",
    errorColor: "#ee0000",
    borderRadius: "6px",
    borderRadiusSmall: "4px",
    fontWeightStrong: "600",
    fontFamily: systemFont,
  },
  Card: {
    paddingMedium: "24px",
    borderRadius: "8px",
  },
  Button: {
    fontWeight: "500",
    heightMedium: "36px",
    heightLarge: "44px",
    borderRadiusMedium: "6px",
    borderRadiusLarge: "6px",
  },
  Input: {
    heightMedium: "36px",
    heightLarge: "44px",
  },
  Menu: {
    itemHeight: "36px",
    borderRadius: "6px",
  },
  DataTable: {
    thPaddingSmall: "8px 12px",
    tdPaddingSmall: "8px 12px",
  },
  Dialog: {
    borderRadius: "12px",
  },
  Tag: {
    borderRadius: "4px",
  },
  LoadingBar: {
    colorLoading: "#0070f3",
    colorError: "#ee0000",
    height: "2px",
  },
} satisfies GlobalThemeOverrides;

export const lightThemeOverrides: GlobalThemeOverrides = {
  ...sharedOverrides,
  common: {
    ...sharedOverrides.common,
    bodyColor: "#fafafa",
    cardColor: "#ffffff",
    modalColor: "#ffffff",
    popoverColor: "#ffffff",
    tableColor: "#ffffff",
    textColorBase: "#171717",
    textColor1: "#171717",
    textColor2: "#4d4d4d",
    textColor3: "#888888",
    borderColor: "#ebebeb",
    dividerColor: "#ebebeb",
  },
  Button: {
    ...sharedOverrides.Button,
    colorPrimary: "#171717",
    colorHoverPrimary: "#383838",
    colorPressedPrimary: "#4d4d4d",
    colorFocusPrimary: "#171717",
    textColorPrimary: "#ffffff",
    textColorHoverPrimary: "#ffffff",
    textColorPressedPrimary: "#ffffff",
    textColorFocusPrimary: "#ffffff",
  },
};

export const darkThemeOverrides: GlobalThemeOverrides = {
  ...sharedOverrides,
  common: {
    ...sharedOverrides.common,
    primaryColor: "#ededed",
    primaryColorHover: "#ffffff",
    primaryColorPressed: "#cccccc",
    primaryColorSuppl: "#ededed",
    infoColor: "#3291ff",
    successColor: "#29a383",
    warningColor: "#ffc043",
    errorColor: "#ff6161",
    bodyColor: "#000000",
    cardColor: "#0a0a0a",
    modalColor: "#0a0a0a",
    popoverColor: "#111111",
    tableColor: "#0a0a0a",
    inputColor: "#1a1a1a",
    actionColor: "#1a1a1a",
    textColorBase: "#ededed",
    textColor1: "#ededed",
    textColor2: "#d4d4d4",
    textColor3: "#888888",
    borderColor: "#333333",
    dividerColor: "#333333",
  },
  Button: {
    ...sharedOverrides.Button,
    colorPrimary: "#ededed",
    colorHoverPrimary: "#ffffff",
    colorPressedPrimary: "#cccccc",
    colorFocusPrimary: "#ededed",
    textColorPrimary: "#171717",
    textColorHoverPrimary: "#171717",
    textColorPressedPrimary: "#171717",
    textColorFocusPrimary: "#171717",
  },
  Card: {
    ...sharedOverrides.Card,
    color: "#0a0a0a",
    textColor: "#ededed",
    borderColor: "#333333",
  },
  Input: {
    ...sharedOverrides.Input,
    color: "#1a1a1a",
    textColor: "#ededed",
    colorFocus: "#1a1a1a",
    borderHover: "#4d4d4d",
    borderFocus: "#ededed",
    placeholderColor: "#888888",
  },
  Select: {
    peers: {
      InternalSelection: {
        textColor: "#ededed",
        color: "#1a1a1a",
        placeholderColor: "#888888",
      },
    },
  },
  DataTable: {
    tdColor: "#0a0a0a",
    thColor: "#141414",
    thTextColor: "#ededed",
    tdTextColor: "#ededed",
    borderColor: "#333333",
  },
  Tag: {
    textColor: "#ededed",
  },
  Pagination: {
    itemTextColor: "#a1a1a1",
    itemTextColorActive: "#ededed",
    itemColor: "#1a1a1a",
    itemColorActive: "#2a2a2a",
  },
  DatePicker: {
    itemTextColor: "#ededed",
    itemColorActive: "#1a1a1a",
    panelColor: "#0a0a0a",
  },
  Message: {
    color: "#1a1a1a",
    textColor: "#ededed",
    iconColor: "#ededed",
    borderRadius: "8px",
    colorInfo: "#1a1a1a",
    colorSuccess: "#1a1a1a",
    colorWarning: "#1a1a1a",
    colorError: "#1a1a1a",
    colorLoading: "#1a1a1a",
  },
  LoadingBar: {
    ...sharedOverrides.LoadingBar,
    colorLoading: "#3291ff",
    colorError: "#ff6161",
  },
  Notification: {
    color: "#1a1a1a",
    textColor: "#ededed",
    titleTextColor: "#ededed",
    descriptionTextColor: "#a1a1a1",
    borderRadius: "8px",
  },
};
