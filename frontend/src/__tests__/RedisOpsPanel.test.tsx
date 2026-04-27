import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { RedisOpsPanel } from "../components/query/RedisOpsPanel";
import { useTabStore } from "../stores/tabStore";
import { RedisClientList, RedisCommandHistory, RedisSlowLog } from "../../wailsjs/go/app/App";

describe("RedisOpsPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useTabStore.setState({
      activeTabId: "query-10",
      tabs: [
        {
          id: "query-10",
          type: "query",
          label: "Redis",
          meta: { type: "query", assetId: 10, assetName: "Redis", assetIcon: "", assetType: "redis" },
        },
      ],
    });
  });

  it("loads slowlog clients and command history", async () => {
    vi.mocked(RedisSlowLog).mockResolvedValue([
      {
        id: 1,
        timestamp: 1710000000,
        durationMicros: 2500,
        command: ["GET", "user:1"],
        client: "127.0.0.1:5000",
        clientName: "console",
      },
    ]);
    vi.mocked(RedisClientList).mockResolvedValue("id=3 addr=127.0.0.1:5000 name=console cmd=get");
    vi.mocked(RedisCommandHistory).mockResolvedValue([
      { assetId: 10, db: 0, command: "GET user:1", costMillis: 3, timestamp: 1710000000000 },
    ]);

    render(<RedisOpsPanel tabId="query-10" />);

    await waitFor(() => {
      expect(RedisSlowLog).toHaveBeenCalledWith(10, 128);
      expect(RedisClientList).toHaveBeenCalledWith(10);
      expect(RedisCommandHistory).toHaveBeenCalledWith(10, 50);
    });
    expect(screen.getByText("GET user:1")).toBeInTheDocument();
    expect(screen.getAllByText(/127.0.0.1:5000/).length).toBeGreaterThan(0);
  });
});
