import { memo, useMemo, useState, useCallback } from "react";
import { BarChart3, Check } from "lucide-react";
import { cn } from "@/lib/utils";
import { useChatStore } from "@/stores/chatStore";
import { chatAPI, type PollData } from "@/lib/api";
import { useTranslation } from "@/i18n/context";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";

interface PollMessageProps {
  poll: PollData;
  messageId: string;
}

export const PollMessage = memo(function PollMessage({ poll, messageId }: PollMessageProps) {
  const { t } = useTranslation();
  const username = useChatStore((s) => s.username);
  const [selectedOptions, setSelectedOptions] = useState<Set<number>>(new Set());
  const [hasVoted, setHasVoted] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const userVotedIndices = useMemo(() => {
    if (!username) return new Set<number>();
    const indices = new Set<number>();
    if (poll.voters) {
      for (const [idxStr, voters] of Object.entries(poll.voters)) {
        if (voters.includes(username)) indices.add(Number(idxStr));
      }
    }
    return indices;
  }, [poll.voters, username]);

  const totalVotes = useMemo(
    () => Object.values(poll.votes).reduce((sum, count) => sum + count, 0),
    [poll.votes],
  );

  const handleOptionClick = useCallback(
    (index: number) => {
      if (poll.is_closed) return;
      if (poll.multiple_choice) {
        setSelectedOptions((prev) => {
          const next = new Set(prev);
          if (userVotedIndices.has(index) || next.has(index)) next.delete(index);
          else next.add(index);
          return next;
        });
      } else {
        setSelectedOptions(new Set([index]));
      }
    },
    [poll.is_closed, poll.multiple_choice, userVotedIndices],
  );

  const handleVote = useCallback(async () => {
    if (selectedOptions.size === 0) return;
    setError(null);
    try {
      for (const idx of selectedOptions) {
        await chatAPI.sendPollVote(messageId, idx);
      }
      setHasVoted(true);
      setSelectedOptions(new Set());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [selectedOptions, messageId]);

  const handleClosePoll = useCallback(async () => {
    setError(null);
    try {
      await chatAPI.sendPollClose(messageId);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [messageId]);

  const isOwnPoll = poll.creator === username;
  const isClosed = poll.is_closed;

  return (
    <Card className="w-full max-w-[400px] gap-0 py-0">
      <CardHeader className="gap-2 border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <BarChart3 className="size-3.5 shrink-0 text-muted-foreground" />
          <CardTitle className="text-sm font-semibold leading-snug">{poll.question}</CardTitle>
          {isClosed && (
            <span className="ml-auto shrink-0 text-label text-muted-foreground">
              {t("poll.finalResults")}
            </span>
          )}
        </div>
      </CardHeader>

      <CardContent className="space-y-2 px-4 py-3">
        {poll.options.map((option, idx) => {
          const voteCount = poll.votes[idx] || 0;
          const percentage = totalVotes > 0 ? (voteCount / totalVotes) * 100 : 0;
          const isUserVoted = userVotedIndices.has(idx);
          const isSelected = !isClosed && selectedOptions.has(idx);
          const showBar = totalVotes > 0 || isClosed;
          return (
            <Button
              key={idx}
              type="button"
              variant="outline"
              onClick={() => handleOptionClick(idx)}
              disabled={isClosed || (hasVoted && !poll.multiple_choice)}
              className={cn(
                "relative h-auto w-full justify-between overflow-hidden rounded-md px-3 py-2 text-left font-normal",
                isUserVoted && "border-primary/35 bg-primary/8",
                isSelected && !isUserVoted && "border-primary/35 bg-accent",
              )}
            >
              {showBar && (
                <span
                  className={cn(
                    "absolute inset-y-0 left-0 transition-[width] duration-300 ease-out",
                    isUserVoted ? "bg-primary/12" : "bg-primary/5",
                  )}
                  style={{ width: `${percentage}%` }}
                />
              )}
              <span className="relative z-10 flex min-w-0 items-center gap-2">
                {isUserVoted && <Check className="size-3.5 shrink-0 text-primary" />}
                {!isUserVoted && isSelected && (
                  <span className="size-3.5 shrink-0 rounded-full border-2 border-primary bg-primary/20" />
                )}
                <span className="truncate">{option}</span>
              </span>
              {showBar && (
                <span className="relative z-10 ml-2 shrink-0 text-xs tabular-nums text-muted-foreground">
                  {Math.round(percentage)}%
                </span>
              )}
            </Button>
          );
        })}
      </CardContent>

      <CardFooter className="flex items-center gap-2 border-t px-4 py-2.5">
        <span className="text-label text-muted-foreground">
          {t("poll.votes", { count: totalVotes })}
        </span>
        {!isClosed && !hasVoted && selectedOptions.size > 0 && (
          <Button type="button" size="sm" className="ml-auto h-7 rounded-md" onClick={handleVote}>
            {t("poll.vote")}
          </Button>
        )}
        {!isClosed && isOwnPoll && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="ml-auto text-muted-foreground hover:text-destructive"
            onClick={handleClosePoll}
          >
            {t("poll.closed")}
          </Button>
        )}
      </CardFooter>
      {error && (
        <p className="px-4 pb-3 text-xs text-destructive animate-in fade-in-0" role="alert">
          {error}
        </p>
      )}
    </Card>
  );
});
