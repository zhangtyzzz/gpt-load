<script setup lang="ts">
import LogTable from "@/components/logs/LogTable.vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
</script>

<template>
  <section class="page-shell" aria-labelledby="logs-title">
    <header class="page-heading">
      <div>
        <h1 id="logs-title" class="page-title">{{ t("nav.logs") }}</h1>
        <p class="page-description">{{ t("logs.description") }}</p>
      </div>
    </header>
    <log-table />
  </section>
</template>
