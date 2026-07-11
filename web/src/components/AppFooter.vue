<script setup lang="ts">
import { versionService, type VersionInfo } from "@/services/version";
import {
  BugOutline,
  CheckmarkCircleOutline,
  DocumentTextOutline,
  LogoGithub,
  PeopleOutline,
  TimeOutline,
  WarningOutline,
} from "@vicons/ionicons5";
import { NDivider, NIcon, NTooltip } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();

const versionInfo = ref<VersionInfo>({
  currentVersion: "0.1.0",
  latestVersion: null,
  isLatest: false,
  hasUpdate: false,
  releaseUrl: null,
  lastCheckTime: 0,
  status: "checking",
});

const isChecking = ref(false);

// 版本状态配置
const statusConfig = computed(() => ({
  checking: {
    color: "var(--primary-color)",
    icon: TimeOutline,
    text: t("footer.checking"),
  },
  latest: {
    color: "var(--success-color)",
    icon: CheckmarkCircleOutline,
    text: t("footer.latestVersion"),
  },
  "update-available": {
    color: "var(--warning-color)",
    icon: WarningOutline,
    text: t("footer.updateAvailable"),
  },
  error: {
    color: "var(--error-color)",
    icon: WarningOutline,
    text: t("footer.checkFailed"),
  },
}));

const currentStatus = computed(() => statusConfig.value[versionInfo.value.status]);
const versionReleaseUrl = computed(() => {
  if (
    (versionInfo.value.status === "update-available" || versionInfo.value.status === "latest") &&
    versionInfo.value.releaseUrl
  ) {
    return versionInfo.value.releaseUrl;
  }
  return null;
});

const formatVersion = (version: string): string => {
  return version.startsWith("v") ? version : `v${version}`;
};

const checkVersion = async () => {
  if (isChecking.value) {
    return;
  }

  isChecking.value = true;
  try {
    const result = await versionService.checkForUpdates();
    versionInfo.value = result;
  } catch (error) {
    console.warn("Version check failed:", error);
  } finally {
    isChecking.value = false;
  }
};

onMounted(() => {
  checkVersion();
});
</script>

<template>
  <footer class="app-footer">
    <div class="footer-container">
      <!-- 主要信息区 -->
      <div class="footer-main">
        <span class="project-info">
          <a
            href="https://github.com/zhangtyzzz/gpt-load"
            target="_blank"
            rel="noopener noreferrer"
          >
            <b>GPT-Load</b>
          </a>
        </span>

        <n-divider vertical />

        <!-- 版本信息 -->
        <a
          v-if="versionReleaseUrl"
          :href="versionReleaseUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="version-container"
          :aria-label="`${formatVersion(versionInfo.currentVersion)} - ${currentStatus.text}`"
        >
          <n-icon
            :component="currentStatus.icon"
            :color="currentStatus.color"
            :size="14"
            class="version-icon"
            aria-hidden="true"
          />
          <span class="version-text">
            {{ formatVersion(versionInfo.currentVersion) }}
            -
            <span :style="{ color: currentStatus.color }">
              {{ currentStatus.text }}
              <template v-if="versionInfo.status === 'update-available'">
                [{{ formatVersion(versionInfo.latestVersion || "") }}]
              </template>
            </span>
          </span>
        </a>

        <button
          v-else
          type="button"
          class="version-container"
          :class="{ 'version-checking': isChecking }"
          :disabled="isChecking"
          :aria-label="`${formatVersion(versionInfo.currentVersion)} - ${currentStatus.text}`"
          @click="checkVersion"
        >
          <n-icon
            :component="currentStatus.icon"
            :color="currentStatus.color"
            :size="14"
            class="version-icon"
            aria-hidden="true"
          />
          <span class="version-text">
            {{ formatVersion(versionInfo.currentVersion) }}
            -
            <span :style="{ color: currentStatus.color }">{{ currentStatus.text }}</span>
          </span>
        </button>

        <n-divider vertical />

        <!-- 链接区 -->
        <div class="links-container">
          <n-tooltip trigger="hover" placement="top">
            <template #trigger>
              <a
                href="https://github.com/zhangtyzzz/gpt-load#readme"
                target="_blank"
                rel="noopener noreferrer"
                class="footer-link"
              >
                <n-icon :component="DocumentTextOutline" :size="14" class="link-icon" />
                <span>{{ t("footer.docs") }}</span>
              </a>
            </template>
            {{ t("footer.officialDocs") }}
          </n-tooltip>

          <n-tooltip trigger="hover" placement="top">
            <template #trigger>
              <a
                href="https://github.com/zhangtyzzz/gpt-load"
                target="_blank"
                rel="noopener noreferrer"
                class="footer-link"
              >
                <n-icon :component="LogoGithub" :size="14" class="link-icon" />
                <span>GitHub</span>
              </a>
            </template>
            {{ t("footer.viewSource") }}
          </n-tooltip>

          <n-tooltip trigger="hover" placement="top">
            <template #trigger>
              <a
                href="https://github.com/zhangtyzzz/gpt-load/issues"
                target="_blank"
                rel="noopener noreferrer"
                class="footer-link"
              >
                <n-icon :component="BugOutline" :size="14" class="link-icon" />
                <span>{{ t("footer.feedback") }}</span>
              </a>
            </template>
            {{ t("footer.reportIssue") }}
          </n-tooltip>

          <n-tooltip trigger="hover" placement="top">
            <template #trigger>
              <a
                href="https://github.com/zhangtyzzz/gpt-load/graphs/contributors"
                target="_blank"
                rel="noopener noreferrer"
                class="footer-link"
              >
                <n-icon :component="PeopleOutline" :size="14" class="link-icon" />
                <span>{{ t("footer.contributors") }}</span>
              </a>
            </template>
            {{ t("footer.viewContributors") }}
          </n-tooltip>
        </div>

        <n-divider vertical />

        <div class="license-container">
          <span class="maintainer-text">
            {{ t("footer.maintainedBy") }}
            <a
              href="https://github.com/zhangtyzzz"
              target="_blank"
              rel="noopener noreferrer"
              class="maintainer-link"
            >
              zhangtyzzz
            </a>
          </span>
          <span class="license-text">MIT License</span>
        </div>
      </div>
    </div>
  </footer>
</template>

<style scoped>
.app-footer {
  background: var(--footer-bg);
  border-top: 1px solid var(--border-color-light);
  min-height: 3.25rem;
  padding: 0.75rem 1.5rem;
  -webkit-backdrop-filter: blur(var(--material-blur)) saturate(160%);
  backdrop-filter: blur(var(--material-blur)) saturate(160%);
  font-size: 0.875rem;
}

.footer-container {
  max-width: 80rem;
  margin: 0 auto;
}

.footer-main {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-4);
  line-height: 1.4;
}

.project-info {
  color: var(--text-secondary);
  font-weight: 500;
}

.project-info a {
  color: var(--primary-color);
  font-weight: 600;
  text-decoration: none;
}

.project-info a:hover {
  text-decoration: underline;
  text-underline-offset: 0.2em;
}

.version-container {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.3rem 0.5rem;
  border: 0;
  border-radius: var(--border-radius-sm);
  background: transparent;
  color: inherit;
  font: inherit;
  text-decoration: none;
  cursor: pointer;
  transition:
    background-color var(--motion-fast) var(--ease-out),
    opacity var(--motion-fast) var(--ease-out),
    transform var(--motion-fast) var(--ease-out);
}

.version-icon {
  display: flex;
  align-items: center;
}

.version-text {
  font-weight: 500;
  font-size: 0.8125rem;
  color: var(--text-secondary);
  white-space: nowrap;
}

.version-container:hover:not(:disabled) {
  background: var(--hover-bg);
  transform: translateY(-1px);
}

.version-container:active:not(:disabled) {
  transform: scale(0.975);
}

.version-checking {
  opacity: 0.7;
  cursor: progress;
}

.links-container {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.footer-link {
  display: flex;
  align-items: center;
  gap: 4px;
  color: var(--text-secondary);
  text-decoration: none;
  padding: 0.3rem 0.4rem;
  border-radius: var(--border-radius-sm);
  font-size: 0.8125rem;
  white-space: nowrap;
  transition:
    background-color var(--motion-fast) var(--ease-out),
    color var(--motion-fast) var(--ease-out),
    transform var(--motion-fast) var(--ease-out);
}

.footer-link:hover {
  color: var(--primary-color);
  background: var(--hover-bg);
  transform: translateY(-1px);
}

.footer-link:active {
  transform: scale(0.975);
}

.link-icon {
  display: flex;
  align-items: center;
}

.license-container {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.maintainer-text,
.license-text {
  color: var(--text-tertiary);
  font-size: 0.75rem;
}

.maintainer-link {
  color: var(--primary-color);
  font-weight: 600;
  text-decoration: none;
}

.maintainer-link:hover {
  text-decoration: underline;
  text-underline-offset: 0.2em;
}

.license-text {
  white-space: nowrap;
}

@media (max-width: 768px) {
  .app-footer {
    padding: 0.625rem 1rem max(0.625rem, env(safe-area-inset-bottom));
    height: auto;
  }

  .footer-main {
    flex-direction: column;
    gap: var(--space-2);
    text-align: center;
  }

  .footer-main :deep(.n-divider) {
    display: none;
  }

  .links-container {
    gap: var(--space-4);
  }
}

@media (max-width: 480px) {
  .footer-main {
    gap: 0.375rem;
  }

  .links-container {
    flex-wrap: wrap;
    justify-content: center;
    gap: var(--space-3);
  }

  .project-info {
    font-size: 12px;
  }

  .footer-link {
    min-height: 2.75rem;
    font-size: 0.75rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .version-container,
  .footer-link {
    transition: opacity var(--motion-fast) ease;
  }

  .version-container:hover:not(:disabled),
  .version-container:active:not(:disabled),
  .footer-link:hover,
  .footer-link:active {
    transform: none;
  }
}

@media (prefers-reduced-transparency: reduce) {
  .app-footer {
    background: var(--card-bg-solid);
    -webkit-backdrop-filter: none;
    backdrop-filter: none;
  }
}

@media (prefers-contrast: more) {
  .app-footer {
    background: var(--card-bg-solid);
    border-top-color: var(--text-secondary);
  }

  .version-container,
  .footer-link {
    outline: 1px solid var(--border-color);
  }
}
</style>
